package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	protocolv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/protocol/v1"
	"github.com/pood1e/wa-app/internal/waapp/management"
)

// Both backends accept numbered parameters; only their execution adapters differ.
type rowScanner interface{ Scan(...any) error }

type administrationStore struct {
	row  func(context.Context, string, ...any) rowScanner
	exec func(context.Context, string, ...any) (int64, error)
}

func (s *SQLiteStore) DashboardRepository() management.Repository {
	return &administrationStore{
		row: func(ctx context.Context, query string, args ...any) rowScanner {
			return s.queries.QueryRowContext(ctx, query, args...)
		},
		exec: func(ctx context.Context, query string, args ...any) (int64, error) {
			result, err := s.queries.ExecContext(ctx, query, args...)
			if err != nil {
				return 0, err
			}
			return result.RowsAffected()
		},
	}
}

func (s *PostgresStore) DashboardRepository() management.Repository {
	return &administrationStore{
		row: func(ctx context.Context, query string, args ...any) rowScanner {
			return s.db.QueryRow(ctx, query, args...)
		},
		exec: func(ctx context.Context, query string, args ...any) (int64, error) {
			result, err := s.db.Exec(ctx, query, args...)
			return result.RowsAffected(), err
		},
	}
}

func (s *administrationStore) load(ctx context.Context, key string) ([]byte, error) {
	var payload []byte
	err := s.row(ctx, `SELECT payload FROM wa_dashboard_configuration WHERE id=$1`, key).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return payload, err
}

func (s *administrationStore) GetAdministrator(ctx context.Context) (*protocolv1.DashboardAdministrator, error) {
	payload, err := s.load(ctx, "administrator")
	if err != nil || len(payload) == 0 {
		return nil, err
	}
	admin := &protocolv1.DashboardAdministrator{}
	return admin, proto.Unmarshal(payload, admin)
}

func (s *administrationStore) CreateAdministrator(ctx context.Context, admin *protocolv1.DashboardAdministrator) (bool, error) {
	payload, err := proto.Marshal(admin)
	if err != nil {
		return false, err
	}
	count, err := s.exec(ctx, `INSERT INTO wa_dashboard_configuration(id,payload) VALUES ('administrator',$1) ON CONFLICT(id) DO NOTHING`, payload)
	return count == 1, err
}

func (s *administrationStore) SaveDashboardSession(ctx context.Context, session *protocolv1.DashboardSession) error {
	if _, err := s.exec(ctx, `DELETE FROM wa_dashboard_sessions WHERE expires_at <= $1`, time.Now().Unix()); err != nil {
		return err
	}
	_, err := s.exec(ctx, `INSERT INTO wa_dashboard_sessions(token_hash,expires_at) VALUES ($1,$2)`, session.GetTokenHash(), session.GetExpiresAt().AsTime().Unix())
	return err
}

func (s *administrationStore) HasDashboardSession(ctx context.Context, hash string, now time.Time) (bool, error) {
	var count int
	err := s.row(ctx, `SELECT COUNT(*) FROM wa_dashboard_sessions WHERE token_hash=$1 AND expires_at>$2`, hash, now.Unix()).Scan(&count)
	return count == 1, err
}

func (s *administrationStore) DeleteDashboardSession(ctx context.Context, hash string) error {
	_, err := s.exec(ctx, `DELETE FROM wa_dashboard_sessions WHERE token_hash=$1`, hash)
	return err
}

func (s *administrationStore) LoadPrivateConfiguration(ctx context.Context, key string) ([]byte, error) {
	return s.load(ctx, "transport:"+key)
}

func (s *administrationStore) SavePrivateConfiguration(ctx context.Context, key string, payload []byte) error {
	_, err := s.exec(ctx, `INSERT INTO wa_dashboard_configuration(id,payload) VALUES ($1,$2) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, "transport:"+key, payload)
	return err
}

func (s *administrationStore) EnqueueNotification(ctx context.Context, notification *protocolv1.MessageNotification) error {
	payload, err := proto.Marshal(notification)
	if err != nil {
		return err
	}
	_, err = s.exec(ctx, `INSERT INTO wa_message_notifications(id,payload,next_attempt,delivered) VALUES ($1,$2,0,0) ON CONFLICT(id) DO NOTHING`, notification.GetNotificationId(), payload)
	return err
}

func (s *administrationStore) PendingNotifications(ctx context.Context, now time.Time, limit int) ([]*protocolv1.MessageNotification, error) {
	// One row per query keeps the adapter independent of SQL driver row iterators.
	items := make([]*protocolv1.MessageNotification, 0, limit)
	lastID := ""
	for range limit {
		var id string
		var payload []byte
		err := s.row(ctx, `SELECT id,payload FROM wa_message_notifications WHERE delivered=0 AND next_attempt<=$1 AND id>$2 ORDER BY id LIMIT 1`, now.Unix(), lastID).Scan(&id, &payload)
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, err
		}
		item := &protocolv1.MessageNotification{}
		if err := proto.Unmarshal(payload, item); err != nil {
			return nil, err
		}
		items = append(items, item)
		lastID = id
	}
	return items, nil
}

func (s *administrationStore) FinishNotification(ctx context.Context, id string) error {
	_, err := s.exec(ctx, `UPDATE wa_message_notifications SET delivered=1,payload=$1 WHERE id=$2`, []byte{}, id)
	return err
}

func (s *administrationStore) RetryNotification(ctx context.Context, id string, next time.Time) error {
	_, err := s.exec(ctx, `UPDATE wa_message_notifications SET next_attempt=$1 WHERE id=$2`, next.Unix(), id)
	return err
}
