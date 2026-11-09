package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestDashboardFilesystem(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>synthetic dashboard</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('synthetic');")},
	}
	handler := standaloneDashboard(assets)
	for _, tc := range []struct {
		path   string
		status int
		body   string
	}{
		{"/", http.StatusOK, "synthetic dashboard"},
		{"/accounts/synthetic-account", http.StatusOK, "synthetic dashboard"},
		{"/assets/app.js", http.StatusOK, "console.log"},
		{"/assets/missing.js", http.StatusNotFound, "404"},
		{"/assets/../../outside", http.StatusBadRequest, "invalid URL path"},
		{"/dashboard/wa/accounts", http.StatusMovedPermanently, ""},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.body) {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
			if tc.status == http.StatusMovedPermanently && response.Header().Get("Location") != "/" {
				t.Fatal("legacy dashboard path no longer redirects to root")
			}
		})
	}
}
