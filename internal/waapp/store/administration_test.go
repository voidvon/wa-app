package store_test

import (
	"testing"
	"time"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/management"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAdministrationPersistence(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, reopen := openRepository(t, backend)
			storage := repo.(interface{ DashboardRepository() management.Repository }).DashboardRepository()
			now := time.Now()
			admin := &protocolv1.DashboardAdministrator{Username: "fixture-admin", PasswordHash: []byte("synthetic-hash"), CreatedAt: timestamppb.New(now)}
			if created, err := storage.CreateAdministrator(t.Context(), admin); err != nil || !created {
				t.Fatal("create admin failed", err)
			}
			if created, err := storage.CreateAdministrator(t.Context(), admin); err != nil || created {
				t.Fatal("administrator overwritten", err)
			}
			if err := storage.SaveDashboardSession(t.Context(), &protocolv1.DashboardSession{TokenHash: "synthetic-hash", ExpiresAt: timestamppb.New(now.Add(time.Hour)), AbsoluteExpiresAt: timestamppb.New(now.Add(8 * time.Hour))}); err != nil {
				t.Fatal(err)
			}
			if err := storage.SavePrivateConfiguration(t.Context(), "feishu", []byte(`{"enabled":false}`)); err != nil {
				t.Fatal(err)
			}
			notification := &protocolv1.MessageNotification{NotificationId: "fixture-event", Text: "synthetic text", ReceivedAt: timestamppb.New(now)}
			for range 2 {
				if err := storage.EnqueueNotification(t.Context(), notification); err != nil {
					t.Fatal(err)
				}
			}
			storage = reopen().(interface{ DashboardRepository() management.Repository }).DashboardRepository()
			loaded, err := storage.GetAdministrator(t.Context())
			if err != nil || loaded.GetUsername() != admin.GetUsername() {
				t.Fatal("administrator lost on restart", err)
			}
			session, err := storage.GetDashboardSession(t.Context(), "synthetic-hash", now)
			if err != nil || session == nil {
				t.Fatal("session lost on restart", err)
			}
			if session, err := storage.GetDashboardSession(t.Context(), "synthetic-hash", now.Add(2*time.Hour)); err != nil || session != nil {
				t.Fatal("expired session accepted", err)
			}
			if err := storage.DeleteDashboardSession(t.Context(), "synthetic-hash"); err != nil {
				t.Fatal(err)
			}
			if session, err := storage.GetDashboardSession(t.Context(), "synthetic-hash", now); err != nil || session != nil {
				t.Fatal("session revocation failed", err)
			}
			payload, err := storage.LoadPrivateConfiguration(t.Context(), "feishu")
			if err != nil || string(payload) != `{"enabled":false}` {
				t.Fatal("configuration lost on restart", err)
			}
			pending, err := storage.PendingNotifications(t.Context(), now, 10)
			if err != nil || len(pending) != 1 {
				t.Fatal("notification not durable or not deduplicated", err)
			}
			if err := storage.RetryNotification(t.Context(), notification.GetNotificationId(), now.Add(time.Hour)); err != nil {
				t.Fatal(err)
			}
			if pending, err := storage.PendingNotifications(t.Context(), now, 10); err != nil || len(pending) != 0 {
				t.Fatal("notification retry ignored backoff", err)
			}
			if pending, err := storage.PendingNotifications(t.Context(), now.Add(2*time.Hour), 10); err != nil || len(pending) != 1 {
				t.Fatal("pending retry was lost", err)
			}
			if err := storage.FinishNotification(t.Context(), notification.GetNotificationId()); err != nil {
				t.Fatal(err)
			}
			if err := storage.EnqueueNotification(t.Context(), notification); err != nil {
				t.Fatal(err)
			}
			if pending, err := storage.PendingNotifications(t.Context(), now.Add(2*time.Hour), 10); err != nil || len(pending) != 0 {
				t.Fatal("delivered notification replayed", err)
			}
		})
	}
}
