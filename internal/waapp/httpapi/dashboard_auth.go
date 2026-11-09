package httpapi

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/management"
)

type dashboardAuthConfig struct {
	administrator      *management.Administrator
	mu                 sync.Mutex
	attempts           map[string]loginAttempt
	global             loginAttempt
	setupLoopbackProxy bool
}

type loginAttempt struct {
	count int
	until time.Time
}

func (c *dashboardAuthConfig) allowLogin(r *http.Request) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for key, attempt := range c.attempts {
		if !now.Before(attempt.until) {
			delete(c.attempts, key)
		}
	}
	if !now.Before(c.global.until) {
		c.global = loginAttempt{until: now.Add(time.Minute)}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	attempt := c.attempts[host]
	if attempt.until.IsZero() {
		attempt.until = now.Add(time.Minute)
	}
	if attempt.count >= 5 || c.global.count >= 30 || len(c.attempts) >= 1024 {
		return false
	}
	attempt.count++
	c.global.count++
	c.attempts[host] = attempt
	return true
}

func withDashboardAuth(next http.Handler, auth *dashboardAuthConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		initialized, err := auth.administrator.Initialized(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "administrator storage unavailable"})
			return
		}
		if !initialized {
			if r.URL.Path == "/setup" {
				handleDashboardSetup(w, r, auth)
				return
			}
			if wantsDashboardJSON(r) {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "administrator setup required"})
				return
			}
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		switch r.URL.Path {
		case "/setup":
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		case "/login":
			handleDashboardLogin(w, r, auth)
		case "/logout":
			handleDashboardLogout(w, r, auth)
		case "/api/dashboard/session/activity":
			handleDashboardSessionActivity(w, r, auth)
		default:
			if authenticatedDashboardRequest(r, auth) {
				next.ServeHTTP(w, r)
				return
			}
			rejectDashboardAuth(w, r)
		}
	})
}

func localDashboardSetup(r *http.Request, allowLoopbackProxy bool) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	peer := net.ParseIP(host)
	if err != nil || !(peer.IsLoopback() || allowLoopbackProxy && peer.IsPrivate()) {
		return false
	}
	host = r.URL.Hostname()
	if host == "" {
		parsed, parseErr := url.Parse("http://" + r.Host)
		if parseErr != nil {
			return false
		}
		host = parsed.Hostname()
	}
	if host != "localhost" && !net.ParseIP(host).IsLoopback() {
		return false
	}
	for _, header := range []string{"Forwarded", "X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto"} {
		if len(r.Header.Values(header)) != 0 {
			return false
		}
	}
	return true
}

func rejectDashboardAuth(w http.ResponseWriter, r *http.Request) {
	if wantsDashboardJSON(r) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
		return
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(dashboardNextFromRequest(r)), http.StatusSeeOther)
}

func wantsDashboardJSON(r *http.Request) bool {
	return r.Method != http.MethodGet || strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(r.Header.Get("Accept"), "application/json")
}

func dashboardNextFromRequest(r *http.Request) string {
	next := r.URL.RequestURI()
	if next == "" || strings.HasPrefix(next, "/login") || strings.HasPrefix(next, "/logout") {
		return "/"
	}
	return dashboardSafeRedirectTarget(next)
}

func dashboardSafeRedirectTarget(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\\r\n") {
		return "/"
	}
	target, err := url.Parse(raw)
	if err != nil || target.IsAbs() || !strings.HasPrefix(target.Path, "/") || strings.ContainsAny(target.Path, "\\\r\n") || strings.HasPrefix(target.Path, "//") || target.Path == "/login" || target.Path == "/logout" || target.Path == "/setup" {
		return "/"
	}
	return target.RequestURI()
}

func authenticatedDashboardRequest(r *http.Request, auth *dashboardAuthConfig) bool {
	cookie, err := r.Cookie(dashboardSessionCookieName)
	return err == nil && auth.administrator.Authenticated(r.Context(), cookie.Value)
}
