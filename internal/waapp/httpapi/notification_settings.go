package httpapi

import (
	"net/http"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/management"
)

func handleNotificationSettings(w http.ResponseWriter, r *http.Request, notifications *management.Notifications) {
	response := &waappv1.DashboardNotificationSettingsResponse{}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid notification configuration"})
			return
		}
		switch r.FormValue("action") {
		case "test":
			if err := notifications.Test(r.Context()); err != nil {
				response.Error = &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_REJECTED, Message: err.Error(), Retryable: true}
			}
		case "save":
			cfg := notifications.Configuration()
			cfg.Enabled = r.FormValue("enabled") == "on"
			if value := r.FormValue("webhook_url"); value != "" {
				cfg.WebhookURL = value
			}
			if value := r.FormValue("signing_secret"); value != "" {
				cfg.SigningSecret = value
			}
			if r.FormValue("remove_secret") == "on" {
				cfg.SigningSecret = ""
			}
			if err := notifications.SaveConfiguration(r.Context(), cfg); err != nil {
				response.Error = &waappv1.WaError{Code: waappv1.WaErrorCode_WA_ERROR_CODE_VALIDATION_FAILED, Message: err.Error()}
			}
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid notification action"})
			return
		}
	default:
		methodNotAllowed(w, "GET, POST")
		return
	}
	cfg := notifications.Configuration()
	response.Settings = &waappv1.DashboardNotificationSettings{Enabled: cfg.Enabled, WebhookConfigured: cfg.WebhookURL != "", SigningConfigured: cfg.SigningSecret != ""}
	w.Header().Set("Cache-Control", "no-store")
	writeProtoJSON(w, http.StatusOK, response)
}
