package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/management"
)

const dashboardSessionCookieName = "wa_app_session"

func dashboardSessionCookie(r *http.Request, token string) *http.Cookie {
	return &http.Cookie{Name: dashboardSessionCookieName, Value: token, Path: "/", Expires: time.Now().Add(management.SessionDuration), MaxAge: int(management.SessionDuration.Seconds()), HttpOnly: true, Secure: dashboardCookieSecure(r), SameSite: http.SameSiteStrictMode}
}

func expiredDashboardSessionCookie(r *http.Request) *http.Cookie {
	cookie := dashboardSessionCookie(r, "")
	cookie.Expires = time.Unix(0, 0)
	cookie.MaxAge = -1
	return cookie
}

func dashboardCookieSecure(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https")
}
