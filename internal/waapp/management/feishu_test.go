package management

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type notificationRepository struct {
	Repository
	config   []byte
	pending  *protocolv1.MessageNotification
	next     time.Time
	finished bool
}

func (s *notificationRepository) LoadPrivateConfiguration(context.Context, string) ([]byte, error) {
	return s.config, nil
}
func (s *notificationRepository) SavePrivateConfiguration(_ context.Context, _ string, payload []byte) error {
	s.config = payload
	return nil
}
func (s *notificationRepository) EnqueueNotification(_ context.Context, item *protocolv1.MessageNotification) error {
	if s.pending == nil {
		s.pending = item
	}
	return nil
}
func (s *notificationRepository) PendingNotifications(_ context.Context, now time.Time, _ int) ([]*protocolv1.MessageNotification, error) {
	if s.pending != nil && !s.finished && !now.Before(s.next) {
		return []*protocolv1.MessageNotification{s.pending}, nil
	}
	return nil, nil
}
func (s *notificationRepository) FinishNotification(context.Context, string) error {
	s.finished = true
	return nil
}
func (s *notificationRepository) RetryNotification(_ context.Context, _ string, next time.Time) error {
	s.next = next
	return nil
}

type notificationTransport func(*http.Request) (*http.Response, error)

func (f notificationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFeishuSigningResponseValidationAndSecretRedaction(t *testing.T) {
	repository := &notificationRepository{}
	notifications, err := NewNotifications(t.Context(), repository)
	if err != nil {
		t.Fatal(err)
	}
	cfg := FeishuConfig{Enabled: true, WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-token", SigningSecret: "synthetic-signing-secret"}
	if err := notifications.SaveConfiguration(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	responseBody := `{"code":0}`
	notifications.client.Transport = notificationTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid webhook request")
		}
		var payload feishuRequest
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		mac := hmac.New(sha256.New, []byte(payload.Timestamp+"\n"+cfg.SigningSecret))
		if payload.Sign != base64.StdEncoding.EncodeToString(mac.Sum(nil)) || payload.Timestamp == "" {
			t.Error("invalid Feishu signature")
		}
		if payload.MessageType != "text" || !strings.Contains(payload.Content.Text, "WA") {
			t.Error("invalid Feishu message shape")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(responseBody)), Header: make(http.Header)}, nil
	})
	for _, body := range []string{`{"code":0}`, `{"StatusCode":0}`} {
		responseBody = body
		if err := notifications.Test(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{`{"code":19021,"msg":"synthetic-token"}`, `{"StatusCode":19022}`, `{}`, `invalid`} {
		responseBody = body
		err := notifications.Test(t.Context())
		if err == nil || strings.Contains(err.Error(), "synthetic-token") {
			t.Fatal("invalid response accepted or secret exposed")
		}
	}
	notifications.client.Transport = notificationTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New(cfg.WebhookURL) })
	if err := notifications.Test(t.Context()); err == nil || strings.Contains(err.Error(), "synthetic-token") {
		t.Fatal("network error exposed webhook")
	}
	for _, target := range []string{"http://127.0.0.1/hook", "https://open.feishu.cn.attacker.example/hook", "https://open.feishu.cn/open-apis/bot/v2/hook/token?secret=value", "https://open.feishu.cn/other/token", "https://open.feishu.cn/open-apis/bot/v2/hook/a%2fb"} {
		if err := validateFeishuConfig(FeishuConfig{Enabled: true, WebhookURL: target}); err == nil {
			t.Fatal("untrusted webhook accepted")
		}
	}
}

func TestFeishuDeliveryRetryDisableAndRestart(t *testing.T) {
	repo := &notificationRepository{}
	n, err := NewNotifications(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := n.SaveConfiguration(t.Context(), FeishuConfig{Enabled: true, WebhookURL: "https://open.feishu.cn/open-apis/bot/v2/hook/synthetic-token"}); err != nil {
		t.Fatal(err)
	}
	session, msg, decrypted := notificationFixture()
	if err := n.Enqueue(t.Context(), session, msg, decrypted); err != nil {
		t.Fatal(err)
	}
	calls := 0
	n.client.Transport = notificationTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("synthetic failure") })
	if err := n.deliverNext(t.Context()); err == nil || calls != 1 || repo.next.Before(time.Now()) {
		t.Fatal("failed delivery did not persist retry")
	}
	if err := n.deliverNext(t.Context()); err != nil || calls != 1 {
		t.Fatal("retry backoff ignored")
	}
	restarted, err := NewNotifications(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	cfg := restarted.Configuration()
	cfg.Enabled = false
	if err := restarted.SaveConfiguration(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	repo.next = time.Time{}
	if err := restarted.deliverNext(t.Context()); err != nil || repo.finished {
		t.Fatal("disabled webhook delivered messages")
	}
	cfg.Enabled = true
	if err := restarted.SaveConfiguration(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	restarted.client.Transport = notificationTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":0}`))}, nil
	})
	if err := restarted.deliverNext(t.Context()); err != nil || !repo.finished {
		t.Fatal("persisted notification was not delivered after restart", err)
	}
}

func notificationFixture() (*waappv1.MessageSession, *waappv1.InboundMessage, *waappv1.DecryptedMessage) {
	return &waappv1.MessageSession{WaAccountId: "fixture-account"}, &waappv1.InboundMessage{MessageId: "fixture-message", ProviderMessageId: "provider-1", Kind: waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE, Direction: waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND, Source: waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION, ReceivedAt: timestamppb.Now()}, &waappv1.DecryptedMessage{Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextText: &waappv1.SensitiveText{Value: "synthetic text"}}
}

func TestNotificationMessageFilteringAndDeduplication(t *testing.T) {
	session, msg, decrypted := notificationFixture()
	original := BuildMessageNotification(session, msg, decrypted)
	msg.MessageId = "redelivered-message"
	if got := BuildMessageNotification(session, msg, decrypted); got.GetNotificationId() != original.GetNotificationId() {
		t.Fatal("provider redelivery was not deduplicated")
	}
	for _, kind := range []waappv1.InboundMessageKind{waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_NOTIFICATION, waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_RECEIPT, waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_SYSTEM} {
		msg.Kind = kind
		if BuildMessageNotification(session, msg, decrypted) != nil {
			t.Fatal("non-message generated notification")
		}
	}
	msg.Kind = waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE
	for _, direction := range []waappv1.AccountMessageDirection{waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_OUTBOUND, waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_SYSTEM} {
		msg.Direction = direction
		if BuildMessageNotification(session, msg, decrypted) != nil {
			t.Fatal("outbound/system message generated notification")
		}
	}
	msg.Direction = waappv1.AccountMessageDirection_ACCOUNT_MESSAGE_DIRECTION_INBOUND
	msg.Source = waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_IMPORTED_HISTORY
	if BuildMessageNotification(session, msg, decrypted) != nil {
		t.Fatal("history generated notification")
	}
	msg.Source = waappv1.AccountMessageSource_ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION
	decrypted.Content = &waappv1.MessageContent{Type: waappv1.MessageContentType_MESSAGE_CONTENT_TYPE_CONTROL}
	if BuildMessageNotification(session, msg, decrypted) != nil {
		t.Fatal("control message generated notification")
	}
	decrypted.Content = nil
	decrypted.Status = waappv1.DecryptionStatus_DECRYPTION_STATUS_FAILED
	if BuildMessageNotification(session, msg, decrypted) != nil {
		t.Fatal("failed decryption generated notification")
	}
	if text := notificationText(&protocolv1.MessageNotification{Text: strings.Repeat("测", 3100) + "<at user_id=all>all</at>"}); len([]rune(text)) > 3300 {
		t.Fatal("notification exceeded text limit")
	}
}
