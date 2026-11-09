package httpapi

import (
	"errors"
	"html/template"
	"net/http"

	"github.com/pood1e/wa-app/internal/waapp/management"
)

const dashboardLoginMaxFormBytes = 4096

type dashboardLoginPageData struct {
	Next  string
	Error string
	Setup bool
}

var dashboardLoginTemplate = template.Must(template.New("dashboard-login").Parse(dashboardLoginHTML))

func handleDashboardSetup(w http.ResponseWriter, r *http.Request, auth *dashboardAuthConfig) {
	if !localDashboardSetup(r, auth.setupLoopbackProxy) {
		http.Error(w, "首次设置仅允许本机访问。远程服务器请使用 SSH 端口转发后打开 http://127.0.0.1:8080/setup。", http.StatusForbidden)
		return
	}
	if r.Method == http.MethodGet {
		renderDashboardAuth(w, http.StatusOK, "/", "", true)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !auth.allowLogin(r) {
		w.Header().Set("Retry-After", "60")
		renderDashboardAuth(w, http.StatusTooManyRequests, "/", "操作过于频繁，请一分钟后重试", true)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, dashboardLoginMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		renderDashboardAuth(w, http.StatusBadRequest, "/", "设置请求无效", true)
		return
	}
	if r.FormValue("password") != r.FormValue("confirm_password") {
		renderDashboardAuth(w, http.StatusBadRequest, "/", "两次输入的密码不一致", true)
		return
	}
	if err := auth.administrator.Setup(r.Context(), r.FormValue("username"), r.FormValue("password")); err != nil {
		renderDashboardAuth(w, http.StatusBadRequest, "/", err.Error(), true)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func handleDashboardLogin(w http.ResponseWriter, r *http.Request, auth *dashboardAuthConfig) {
	next := dashboardSafeRedirectTarget(r.URL.Query().Get("next"))
	switch r.Method {
	case http.MethodGet:
		if authenticatedDashboardRequest(r, auth) {
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
		renderDashboardAuth(w, http.StatusOK, next, "", false)
	case http.MethodPost:
		handleDashboardLoginPost(w, r, auth, next)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handleDashboardLoginPost(w http.ResponseWriter, r *http.Request, auth *dashboardAuthConfig, next string) {
	if !auth.allowLogin(r) {
		w.Header().Set("Retry-After", "60")
		renderDashboardAuth(w, http.StatusTooManyRequests, next, "登录过于频繁，请一分钟后重试", false)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, dashboardLoginMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		renderDashboardAuth(w, http.StatusBadRequest, next, "登录请求无效", false)
		return
	}
	next = dashboardSafeRedirectTarget(r.FormValue("next"))
	token, session, err := auth.administrator.Login(r.Context(), r.FormValue("username"), r.FormValue("password"), r.FormValue("remember_device") == "on")
	if err != nil {
		if errors.Is(err, management.ErrInvalidCredentials) {
			renderDashboardAuth(w, http.StatusUnauthorized, next, "用户名或密码不正确", false)
		} else {
			renderDashboardAuth(w, http.StatusServiceUnavailable, next, "登录服务暂时不可用", false)
		}
		return
	}
	http.SetCookie(w, dashboardSessionCookie(r, token, session))
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func handleDashboardLogout(w http.ResponseWriter, r *http.Request, auth *dashboardAuthConfig) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if cookie, err := r.Cookie(dashboardSessionCookieName); err == nil {
		if err := auth.administrator.Logout(r.Context(), cookie.Value); err != nil {
			http.Error(w, "退出失败，请重试", http.StatusServiceUnavailable)
			return
		}
	}
	http.SetCookie(w, expiredDashboardSessionCookie(r))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func renderDashboardAuth(w http.ResponseWriter, status int, next, message string, setup bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = dashboardLoginTemplate.Execute(w, dashboardLoginPageData{Next: next, Error: message, Setup: setup})
}

const dashboardLoginHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>WA 登录</title>
<style>
*{box-sizing:border-box}:root{color-scheme:light dark;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:#07130d;color:#e9fff0}body{min-height:100dvh;margin:0;display:grid;place-items:center;background:radial-gradient(circle at top,#14532d 0,#07130d 48%,#020617 100%)}main{width:min(92vw,380px);border:1px solid rgba(255,255,255,.12);border-radius:24px;background:rgba(2,6,23,.76);box-shadow:0 24px 70px rgba(0,0,0,.36);padding:32px;backdrop-filter:blur(14px)}.brand{display:flex;align-items:center;gap:12px;margin-bottom:24px}.mark{display:grid;place-items:center;width:44px;height:44px;border-radius:14px;background:#22c55e;color:#052e16;font-weight:800}.title{margin:0;font-size:22px}.sub{margin:4px 0 0;color:#a7f3d0;font-size:13px}label{display:grid;gap:8px;margin-top:16px;color:#bbf7d0;font-size:13px}input{box-sizing:border-box;width:100%;border:1px solid rgba(187,247,208,.22);border-radius:12px;background:rgba(15,23,42,.92);color:#f8fafc;padding:12px 14px;font:inherit;outline:none}input:focus{border-color:#22c55e;box-shadow:0 0 0 3px rgba(34,197,94,.18)}button{width:100%;margin-top:22px;border:0;border-radius:12px;background:#22c55e;color:#052e16;padding:12px 14px;font:inherit;font-weight:700;cursor:pointer}button:hover{background:#4ade80}.hint{margin:14px 0 0;color:#86efac;font-size:12px;line-height:1.6}.error{margin:0 0 14px;border:1px solid rgba(248,113,113,.28);border-radius:12px;background:rgba(127,29,29,.32);color:#fecaca;padding:10px 12px;font-size:13px}
.remember{display:flex;align-items:center;gap:8px}.remember input{width:auto;margin:0;accent-color:#22c55e}
</style>
</head>
<body>
<main>
  <div class="brand"><div class="mark">WA</div><div><h1 class="title">{{if .Setup}}设置管理员{{else}}登录 WA 管理{{end}}</h1><p class="sub">{{if .Setup}}首次使用，请创建管理员账号{{else}}输入用户名和密码{{end}}</p></div></div>
  {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
  <form method="post" action="{{if .Setup}}/setup{{else}}/login{{end}}" autocomplete="on">
    <input type="hidden" name="next" value="{{.Next}}">
    <label>用户名<input name="username" type="text" autocomplete="username" minlength="3" maxlength="64" required autofocus></label>
    <label>密码<input name="password" type="password" autocomplete="{{if .Setup}}new-password{{else}}current-password{{end}}" {{if .Setup}}minlength="12"{{end}} required></label>
    {{if .Setup}}<label>确认密码<input name="confirm_password" type="password" autocomplete="new-password" minlength="12" required></label>{{end}}
    {{if not .Setup}}<label class="remember"><input name="remember_device" type="checkbox">记住此设备</label>{{end}}
    <button type="submit">{{if .Setup}}创建管理员{{else}}登录{{end}}</button>
    <p class="hint">{{if .Setup}}密码至少 12 字节，最长 72 字节。管理员信息保存在当前数据库。{{else}}普通登录：闲置 30 分钟过期，最长 8 小时。<br>记住此设备：闲置 7 天过期，使用时续期，最长 30 天。请仅在个人设备上勾选。{{end}}</p>
  </form>
</main>
</body>
</html>`
