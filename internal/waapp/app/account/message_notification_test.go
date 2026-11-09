package account

import (
	"context"
	"errors"
	"testing"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
	"github.com/pood1e/wa-app/internal/waapp/shared"
	"github.com/pood1e/wa-app/internal/waapp/wacore"
)

type notificationMessageRepository struct {
	Repository
	message *waappv1.InboundMessage
	saved   bool
}

func (r *notificationMessageRepository) GetInboundMessage(context.Context, string) (*waappv1.InboundMessage, error) {
	return r.message, nil
}
func (r *notificationMessageRepository) GetMessageSession(context.Context, string) (*waappv1.MessageSession, error) {
	return &waappv1.MessageSession{WaAccountId: "fixture-account"}, nil
}
func (r *notificationMessageRepository) SaveDecryptedMessage(context.Context, *waappv1.DecryptedMessage) error {
	r.saved = true
	return nil
}
func (r *notificationMessageRepository) SaveInboundMessages(context.Context, []*waappv1.InboundMessage) error {
	return nil
}
func (r *notificationMessageRepository) SaveWAContacts(context.Context, []*waappv1.WAContact) error {
	return nil
}

type notificationDecryptRunner struct {
	wacore.Communication
	fail bool
}

func (r notificationDecryptRunner) DecryptMessage(context.Context, wacore.EngineDecryptInput) wacore.EngineDecryptResult {
	if r.fail {
		return wacore.EngineDecryptResult{Err: errors.New("synthetic decryption failure")}
	}
	return wacore.EngineDecryptResult{MessageKind: waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE, DecryptedMessage: &waappv1.DecryptedMessage{Status: waappv1.DecryptionStatus_DECRYPTION_STATUS_DECRYPTED, PlaintextText: &waappv1.SensitiveText{Value: "synthetic text"}}}
}

func TestAutomaticDecryptionQueuesNotificationAfterPersistence(t *testing.T) {
	repository := &notificationMessageRepository{message: &waappv1.InboundMessage{MessageId: "fixture-message", Kind: waappv1.InboundMessageKind_INBOUND_MESSAGE_KIND_MESSAGE}}
	core := &serverCore{store: repository, clock: shared.SystemClock{}}
	queued := 0
	core.messageNotifier = func(_ context.Context, session *waappv1.MessageSession, msg *waappv1.InboundMessage, decrypted *waappv1.DecryptedMessage) error {
		if !repository.saved || session.GetWaAccountId() != "fixture-account" || msg.GetMessageId() != "fixture-message" || decrypted.GetPlaintextText().GetValue() != "synthetic text" {
			t.Fatal("notification was queued without the persisted message")
		}
		queued++
		return nil
	}
	request := &waappv1.DecryptMessageRequest{MessageId: "fixture-message"}
	response, err := core.DecryptWithProtocol(t.Context(), request, notificationDecryptRunner{}, waappv1.WaOtpSource_WA_OTP_SOURCE_LONG_CONNECTION)
	if err != nil || response.GetError() != nil || queued != 1 {
		t.Fatal("automatic receive did not queue notification", err)
	}
	response, err = core.DecryptWithProtocol(t.Context(), request, notificationDecryptRunner{}, waappv1.WaOtpSource_WA_OTP_SOURCE_AUTO_EXTRACTION)
	if err != nil || response.GetError() != nil || queued != 1 {
		t.Fatal("manual extraction queued notification", err)
	}
	response, err = core.DecryptWithProtocol(t.Context(), request, notificationDecryptRunner{fail: true}, waappv1.WaOtpSource_WA_OTP_SOURCE_LONG_CONNECTION)
	if err != nil || response.GetError() == nil || queued != 1 {
		t.Fatal("failed decryption queued notification", err)
	}
}
