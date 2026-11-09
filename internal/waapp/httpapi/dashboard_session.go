package httpapi

import (
	"net/http"
	"strings"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
)

const dashboardSessionCookieName = "wa_app_session"

func dashboardSessionCookie(r *http.Request, token string, session *protocolv1.DashboardSession) *http.Cookie {
	cookie := &http.Cookie{Name: dashboardSessionCookieName, Value: token, Path: "/", HttpOnly: true, Secure: dashboardCookieSecure(r), SameSite: http.SameSiteStrictMode}
	if session.GetRememberDevice() {
		cookie.Expires = session.GetExpiresAt().AsTime()
		cookie.MaxAge = max(1, int(time.Until(cookie.Expires).Seconds()))
	}
	return cookie
}

func expiredDashboardSessionCookie(r *http.Request) *http.Cookie {
	cookie := dashboardSessionCookie(r, "", nil)
	cookie.Expires = time.Unix(0, 0)
	cookie.MaxAge = -1
	return cookie
}

// Only this explicit user-activity endpoint renews idle deadlines. Normal API
// requests include polling and must only validate the current session.
func handleDashboardSessionActivity(w http.ResponseWriter, r *http.Request, auth *dashboardAuthConfig) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	cookie, err := r.Cookie(dashboardSessionCookieName)
	if err == nil {
		if session := auth.administrator.Authenticate(r.Context(), cookie.Value, true); session != nil {
			http.SetCookie(w, dashboardSessionCookie(r, cookie.Value, session))
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	http.SetCookie(w, expiredDashboardSessionCookie(r))
	rejectDashboardAuth(w, r)
}

func dashboardCookieSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
