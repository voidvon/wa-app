package httpapi

import (
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"testing"

	"github.com/pood1e/wa-app/internal/waapp/management"
	"github.com/pood1e/wa-app/internal/waapp/store"
)

func TestNotificationSettingsMaskAndPreserveSecrets(t *testing.T) {
	repo, err := store.NewSQLiteStoreFile(t.Context(), filepath.Join(t.TempDir(), "notifications.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	notifications, err := management.NewNotifications(t.Context(), repo.DashboardRepository())
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"enabled": {"on"}, "webhook_url": {"https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-token"}, "signing_secret": {"synthetic-signing-secret"}}
	submit := func(values url.Values) *httptest.ResponseRecorder {
		values.Set("action", "save")
		request := httptest.NewRequest("POST", "http://localhost/api/wa/notification-settings", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		result := httptest.NewRecorder()
		handleNotificationSettings(result, request, notifications)
		return result
	}
	result := submit(form)
	if result.Code != 200 || !notifications.Configuration().Enabled {
		t.Fatal("configuration save failed")
	}
	if strings.Contains(result.Body.String(), "synthetic-token") || strings.Contains(result.Body.String(), "synthetic-signing-secret") {
		t.Fatal("configuration secrets exposed in response")
	}
	var response waappv1.DashboardNotificationSettingsResponse
	if err := protojson.Unmarshal(result.Body.Bytes(), &response); err != nil || response.GetError() != nil || !response.GetSettings().GetWebhookConfigured() || !response.GetSettings().GetSigningConfigured() || !response.GetSettings().GetEnabled() {
		t.Fatal("saved configuration did not return safe status flags", err)
	}
	form = url.Values{"webhook_url": {""}, "signing_secret": {""}}
	if result := submit(form); result.Code != 200 {
		t.Fatal("disable failed")
	}
	cfg := notifications.Configuration()
	if cfg.Enabled || !strings.Contains(cfg.WebhookURL, "synthetic-token") || cfg.SigningSecret != "synthetic-signing-secret" {
		t.Fatal("empty fields did not preserve configured secrets")
	}
	form.Set("remove_secret", "on")
	if result := submit(form); result.Code != 200 || notifications.Configuration().SigningSecret != "" {
		t.Fatal("secret removal failed")
	}
	reopened, err := management.NewNotifications(t.Context(), repo.DashboardRepository())
	if err != nil || reopened.Configuration().WebhookURL != cfg.WebhookURL {
		t.Fatal("saved configuration did not survive restart", err)
	}
}

func TestNotificationSettingsReadAndValidation(t *testing.T) {
	repo, err := store.NewSQLiteStoreFile(t.Context(), filepath.Join(t.TempDir(), "notifications.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	notifications, err := management.NewNotifications(t.Context(), repo.DashboardRepository())
	if err != nil {
		t.Fatal(err)
	}
	read := httptest.NewRecorder()
	handleNotificationSettings(read, httptest.NewRequest("GET", "http://localhost/api/wa/notification-settings", nil), notifications)
	var response waappv1.DashboardNotificationSettingsResponse
	if err := protojson.Unmarshal(read.Body.Bytes(), &response); err != nil || response.GetSettings() == nil || response.GetSettings().GetEnabled() || response.GetSettings().GetWebhookConfigured() {
		t.Fatal("initial configuration projection invalid", err)
	}
	if read.Header().Get("Content-Type") != "application/json" || read.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("settings response may be cached or was not JSON")
	}
	for _, action := range []string{"save", "test", "invalid"} {
		r := httptest.NewRequest("POST", "http://localhost/api/wa/notification-settings", strings.NewReader(url.Values{"action": {action}, "enabled": {"on"}, "webhook_url": {"https://invalid.example/synthetic-token"}}.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		result := httptest.NewRecorder()
		handleNotificationSettings(result, r, notifications)
		if action == "invalid" {
			if result.Code != 400 {
				t.Fatal("invalid action accepted")
			}
			continue
		}
		response.Reset()
		if err := protojson.Unmarshal(result.Body.Bytes(), &response); err != nil || response.GetError() == nil || strings.Contains(result.Body.String(), "synthetic-token") {
			t.Fatal("configuration error was missing or exposed submitted secrets", err)
		}
	}
	if notifications.Configuration().Enabled {
		t.Fatal("invalid save modified persisted configuration")
	}
	result := httptest.NewRecorder()
	handleNotificationSettings(result, httptest.NewRequest("DELETE", "http://localhost/api/wa/notification-settings", nil), notifications)
	if result.Code != 405 || result.Header().Get("Allow") != "GET, POST" {
		t.Fatal("unsupported method accepted")
	}
}
