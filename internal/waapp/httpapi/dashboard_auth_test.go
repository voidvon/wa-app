package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pood1e/wa-app/internal/waapp/management"
	"github.com/pood1e/wa-app/internal/waapp/store"
)

func TestDashboardMandatorySetupLoginAndLogout(t *testing.T) {
	repo, err := store.NewSQLiteStoreFile(t.Context(), filepath.Join(t.TempDir(), "dashboard.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	admin, err := management.NewAdministrator(repo.DashboardRepository())
	if err != nil {
		t.Fatal(err)
	}
	auth := &dashboardAuthConfig{administrator: admin, attempts: make(map[string]loginAttempt)}
	handler := http.NewCrossOriginProtection().Handler(withDashboardAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }), auth))
	request := func(method, path, body, remote, origin string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
		r.RemoteAddr = remote
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
	const local = "127.0.0.1:32100"
	const remote = "203.0.113.5:32100"
	if w := request("GET", "/api/wa/accounts", "", local, "", nil); w.Code != 401 {
		t.Fatalf("uninitialized API returned %d", w.Code)
	}
	if w := request("GET", "/", "", local, "", nil); w.Header().Get("Location") != "/setup" {
		t.Fatal("first visit did not require setup")
	}
	if w := request("GET", "/setup", "", remote, "", nil); w.Code != 403 {
		t.Fatal("remote visitor could initialize administrator")
	}
	if w := request("GET", "/healthz", "", remote, "", nil); w.Code != 200 {
		t.Fatal("liveness unavailable before setup")
	}
	credentials := url.Values{"username": {"fixture-admin"}, "password": {"synthetic-password-123"}, "confirm_password": {"synthetic-password-123"}}.Encode()
	if w := request("POST", "/setup", credentials, local, "https://attacker.example", nil); w.Code != 403 {
		t.Fatal("cross origin setup was allowed")
	}
	if w := request("POST", "/setup", credentials, local, "", nil); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatalf("setup failed: %d", w.Code)
	}
	if err := admin.Setup(t.Context(), "second-admin", "synthetic-password-456"); err != management.ErrAlreadyInitialized {
		t.Fatal("existing administrator could be overwritten")
	}
	if w := request("GET", "/api/wa/accounts", "", local, "", nil); w.Code != 401 {
		t.Fatal("initialized API did not require login")
	}
	wrong := url.Values{"username": {"wrong-admin"}, "password": {"synthetic-password-123"}}.Encode()
	if w := request("POST", "/login", wrong, local, "", nil); w.Code != 401 {
		t.Fatal("username was not checked")
	}
	loggedIn := request("POST", "/login", credentials, local, "", nil)
	if loggedIn.Code != 303 {
		t.Fatalf("login failed: %d", loggedIn.Code)
	}
	cookies := loggedIn.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("invalid session cookie")
	}
	cookie := cookies[0]
	if w := request("GET", "/api/wa/accounts", "", local, "", cookie); w.Code != 200 {
		t.Fatal("authenticated request denied")
	}
	restarted, err := management.NewAdministrator(repo.DashboardRepository())
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Authenticated(t.Context(), cookie.Value) {
		t.Fatal("session did not persist across administrator restart")
	}
	if w := request("POST", "/logout", "", local, "https://attacker.example", cookie); w.Code != 403 {
		t.Fatal("cross origin logout was allowed")
	}
	if w := request("POST", "/logout", "", local, "", cookie); w.Code != 303 {
		t.Fatal("logout failed")
	}
	if restarted.Authenticated(t.Context(), cookie.Value) {
		t.Fatal("logout did not revoke the server session")
	}
	if w := request("GET", "/api/wa/accounts", "", local, "", cookie); w.Code != 401 {
		t.Fatal("revoked session replay accepted")
	}
}

func TestLocalSetupRejectsProxyAndHostSpoofing(t *testing.T) {
	for _, host := range []string{"localhost:8080", "127.0.0.1:8080", "[::1]:8080", "public.example"} {
		r := httptest.NewRequest("GET", "http://"+host+"/setup", nil)
		r.RemoteAddr = "127.0.0.1:23456"
		if localDashboardSetup(r, false) != (host != "public.example") {
			t.Fatalf("unexpected local setup result for %s", host)
		}
		r.Header.Set("X-Forwarded-For", "203.0.113.2")
		if localDashboardSetup(r, false) {
			t.Fatal("proxied setup allowed")
		}
	}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8080/setup", nil)
	r.RemoteAddr = "172.18.0.1:23456"
	if localDashboardSetup(r, false) || !localDashboardSetup(r, true) {
		t.Fatal("Docker loopback forwarding did not require explicit configuration")
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	if localDashboardSetup(r, true) {
		t.Fatal("public reverse proxy could initialize the administrator")
	}
	r.Header.Set("X-Forwarded-Proto", "")
	if localDashboardSetup(r, true) {
		t.Fatal("empty forwarding header could bypass local initialization protection")
	}
}

func TestDashboardLoginRateLimit(t *testing.T) {
	auth := &dashboardAuthConfig{attempts: make(map[string]loginAttempt)}
	r := httptest.NewRequest("POST", "http://localhost/login", nil)
	for range 5 {
		if !auth.allowLogin(r) {
			t.Fatal("login throttled too early")
		}
	}
	if auth.allowLogin(r) {
		t.Fatal("unbounded password attempts allowed")
	}
}
