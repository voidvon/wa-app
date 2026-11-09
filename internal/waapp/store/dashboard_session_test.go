package store_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/management"
	"github.com/pood1e/wa-app/internal/waapp/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestDashboardSessionRenewalGuards(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			repo, _ := openRepository(t, backend)
			storage := repo.(interface{ DashboardRepository() management.Repository }).DashboardRepository()
			now := time.Now().Add(time.Hour).Truncate(time.Second)
			session := &protocolv1.DashboardSession{TokenHash: "synthetic-session-hash", ExpiresAt: timestamppb.New(now.Add(30 * time.Minute)), AbsoluteExpiresAt: timestamppb.New(now.Add(8 * time.Hour))}
			if err := storage.SaveDashboardSession(t.Context(), session); err != nil {
				t.Fatal(err)
			}
			session.ExpiresAt = timestamppb.New(now.Add(40 * time.Minute))
			if renewed, err := storage.RenewDashboardSession(t.Context(), session, now.Add(10*time.Minute)); err != nil || renewed == nil {
				t.Fatal("live session renewal failed", err)
			}
			session.ExpiresAt = timestamppb.New(now.Add(35 * time.Minute))
			if renewed, err := storage.RenewDashboardSession(t.Context(), session, now.Add(5*time.Minute)); err != nil || !renewed.GetExpiresAt().AsTime().Equal(now.Add(40*time.Minute)) {
				t.Fatal("late activity shortened the session", err)
			}
			session.ExpiresAt = timestamppb.New(now.Add(9 * time.Hour))
			if renewed, err := storage.RenewDashboardSession(t.Context(), session, now); err != nil || renewed != nil {
				t.Fatal("absolute deadline was extended", err)
			}
			session.ExpiresAt = timestamppb.New(now.Add(time.Hour))
			if renewed, err := storage.RenewDashboardSession(t.Context(), session, now.Add(40*time.Minute)); err != nil || renewed != nil {
				t.Fatal("expired session was revived", err)
			}
			if err := storage.DeleteDashboardSession(t.Context(), session.GetTokenHash()); err != nil {
				t.Fatal(err)
			}
			if renewed, err := storage.RenewDashboardSession(t.Context(), session, now); err != nil || renewed != nil {
				t.Fatal("concurrent logout was undone", err)
			}
		})
	}
}

func TestLegacyDashboardSessionsRequireLogin(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var open func() management.Repository
			if backend == "sqlite" {
				path := filepath.Join(t.TempDir(), "legacy.sqlite3")
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.ExecContext(t.Context(), `CREATE TABLE wa_dashboard_sessions (token_hash TEXT PRIMARY KEY, expires_at INTEGER NOT NULL); INSERT INTO wa_dashboard_sessions VALUES ('synthetic-legacy-hash',4102444800)`); err != nil {
					t.Fatal(err)
				}
				open = func() management.Repository {
					repo, err := store.NewSQLiteStoreFile(t.Context(), path)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(repo.Close)
					return repo.DashboardRepository()
				}
			} else {
				dsn := postgresTestDSN(t)
				conn, err := pgx.Connect(t.Context(), dsn)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close(t.Context())
				if _, err := conn.Exec(t.Context(), `CREATE TABLE wa_dashboard_sessions (token_hash TEXT PRIMARY KEY, expires_at BIGINT NOT NULL); INSERT INTO wa_dashboard_sessions VALUES ('synthetic-legacy-hash',4102444800)`); err != nil {
					t.Fatal(err)
				}
				open = func() management.Repository {
					repo, err := store.NewPostgresStore(t.Context(), dsn)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(repo.Close)
					return repo.DashboardRepository()
				}
			}
			storage := open()
			now := time.Now()
			if session, err := storage.GetDashboardSession(t.Context(), "synthetic-legacy-hash", now); err != nil || session != nil {
				t.Fatal("legacy session survived without a fixed deadline", err)
			}
			session := &protocolv1.DashboardSession{TokenHash: "synthetic-new-hash", ExpiresAt: timestamppb.New(now.Add(7 * 24 * time.Hour)), AbsoluteExpiresAt: timestamppb.New(now.Add(30 * 24 * time.Hour)), RememberDevice: true}
			if err := storage.SaveDashboardSession(t.Context(), session); err != nil {
				t.Fatal(err)
			}
			loaded, err := open().GetDashboardSession(t.Context(), session.GetTokenHash(), now)
			if err != nil || loaded == nil || !loaded.GetRememberDevice() || loaded.GetAbsoluteExpiresAt().AsTime().Unix() != session.GetAbsoluteExpiresAt().AsTime().Unix() {
				t.Fatal("new session lost on schema reopen", err)
			}
		})
	}
}
