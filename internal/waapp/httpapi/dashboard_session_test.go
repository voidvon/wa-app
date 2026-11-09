package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pood1e/wa-app/internal/waapp/management"
	"github.com/pood1e/wa-app/internal/waapp/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDashboardLoginModesAndActivity(t *testing.T) {
	repo, err := store.NewSQLiteStoreFile(t.Context(), filepath.Join(t.TempDir(), "dashboard.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	admin, err := management.NewAdministrator(repo.DashboardRepository())
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Setup(t.Context(), "fixture-admin", "synthetic-password-123"); err != nil {
		t.Fatal(err)
	}
	auth := &dashboardAuthConfig{administrator: admin, attempts: make(map[string]loginAttempt)}
	handler := http.NewCrossOriginProtection().Handler(withDashboardAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }), auth))
	request := func(method, path, body, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "https://localhost"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	page := request("GET", "/login", "", "", nil)
	if !strings.Contains(page.Body.String(), `name="remember_device" type="checkbox"`) || strings.Contains(page.Body.String(), `type="checkbox" checked`) {
		t.Fatal("remember-device choice missing or enabled by default")
	}
	for _, remember := range []bool{false, true} {
		name := "ordinary"
		if remember {
			name = "remembered"
		}
		t.Run(name, func(t *testing.T) {
			form := url.Values{"username": {"fixture-admin"}, "password": {"synthetic-password-123"}}
			if remember {
				form.Set("remember_device", "on")
			}
			login := request("POST", "/login", form.Encode(), "", nil)
			if login.Code != http.StatusSeeOther || len(login.Result().Cookies()) != 1 {
				t.Fatal("login failed")
			}
			cookie := login.Result().Cookies()[0]
			if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatal("cookie protections missing")
			}
			if remember != (cookie.MaxAge > 0 && !cookie.Expires.IsZero()) {
				t.Fatal("incorrect cookie persistence")
			}
			session := admin.Authenticate(t.Context(), cookie.Value, false)
			idle, maximum := 30*time.Minute, 8*time.Hour
			if remember {
				idle, maximum = 7*24*time.Hour, 30*24*time.Hour
			}
			if session == nil || session.GetRememberDevice() != remember || time.Until(session.GetExpiresAt().AsTime()) < idle-5*time.Second || time.Until(session.GetAbsoluteExpiresAt().AsTime()) < maximum-5*time.Second {
				t.Fatal("login mode did not set server deadlines")
			}
			// Simulate a session close to its idle deadline; polling cannot renew it.
			session.ExpiresAt = timestamppb.New(time.Now().Add(time.Minute))
			if remember {
				session.AbsoluteExpiresAt = timestamppb.New(time.Now().Add(2 * 24 * time.Hour))
			}
			if err := repo.DashboardRepository().DeleteDashboardSession(t.Context(), session.GetTokenHash()); err != nil {
				t.Fatal(err)
			}
			if err := repo.DashboardRepository().SaveDashboardSession(t.Context(), session); err != nil {
				t.Fatal(err)
			}
			for _, method := range []string{"GET", "POST"} {
				poll := request(method, "/api/wa/contacts/presence", "", "", cookie)
				if poll.Code != 200 || len(poll.Result().Cookies()) != 0 {
					t.Fatal("polling changed the cookie")
				}
			}
			if unchanged := admin.Authenticate(t.Context(), cookie.Value, false); unchanged.GetExpiresAt().AsTime().Unix() != session.GetExpiresAt().AsTime().Unix() {
				t.Fatal("polling renewed the idle deadline")
			}
			if w := request("POST", "/api/dashboard/session/activity", "", "https://attacker.example", cookie); w.Code != 403 {
				t.Fatal("cross-origin activity accepted")
			}
			if w := request("GET", "/api/dashboard/session/activity", "", "", cookie); w.Code != 405 {
				t.Fatal("GET renewed activity")
			}
			activity := request("POST", "/api/dashboard/session/activity", "", "", cookie)
			if activity.Code != 204 || len(activity.Result().Cookies()) != 1 {
				t.Fatal("activity failed to renew cookie")
			}
			renewed := admin.Authenticate(t.Context(), cookie.Value, false)
			if renewed == nil || time.Until(renewed.GetExpiresAt().AsTime()) < 29*time.Minute {
				t.Fatal("activity failed to renew server deadline")
			}
			if remember && (renewed.GetExpiresAt().AsTime().Unix() != session.GetAbsoluteExpiresAt().AsTime().Unix() || activity.Result().Cookies()[0].Expires.Unix() != renewed.GetExpiresAt().AsTime().Unix()) {
				t.Fatal("remembered cookie exceeded the fixed deadline")
			}
			if err := admin.Logout(t.Context(), cookie.Value); err != nil {
				t.Fatal(err)
			}
			if w := request("POST", "/api/dashboard/session/activity", "", "", cookie); w.Code != 401 {
				t.Fatal("revoked session was revived")
			}
		})
	}
	if w := request("POST", "/api/dashboard/session/activity", "", "", nil); w.Code != 401 {
		t.Fatal("activity endpoint allowed anonymous access")
	}
}
