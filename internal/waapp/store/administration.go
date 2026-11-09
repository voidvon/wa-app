package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

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
	if _, err := s.exec(ctx, `DELETE FROM wa_dashboard_sessions WHERE expires_at <= $1 OR absolute_expires_at <= $1`, time.Now().Unix()); err != nil {
		return err
	}
	_, err := s.exec(ctx, `INSERT INTO wa_dashboard_sessions(token_hash,expires_at,absolute_expires_at,remember_device) VALUES ($1,$2,$3,$4)`, session.GetTokenHash(), session.GetExpiresAt().AsTime().Unix(), session.GetAbsoluteExpiresAt().AsTime().Unix(), dashboardRememberDeviceValue(session))
	return err
}

func (s *administrationStore) GetDashboardSession(ctx context.Context, hash string, now time.Time) (*protocolv1.DashboardSession, error) {
	return scanDashboardSession(s.row(ctx, `SELECT expires_at,absolute_expires_at,remember_device FROM wa_dashboard_sessions WHERE token_hash=$1 AND expires_at>$2 AND absolute_expires_at>$2`, hash, now.Unix()), hash)
}

func (s *administrationStore) RenewDashboardSession(ctx context.Context, session *protocolv1.DashboardSession, now time.Time) (*protocolv1.DashboardSession, error) {
	// A concurrent logout or expiry must not be undone, nor may a late request
	// shorten a deadline that another activity request already extended.
	return scanDashboardSession(s.row(ctx, `UPDATE wa_dashboard_sessions SET expires_at=CASE WHEN expires_at>$3 THEN expires_at ELSE $3 END WHERE token_hash=$1 AND expires_at>$2 AND absolute_expires_at>$2 AND absolute_expires_at>=$3 RETURNING expires_at,absolute_expires_at,remember_device`, session.GetTokenHash(), now.Unix(), session.GetExpiresAt().AsTime().Unix()), session.GetTokenHash())
}

func scanDashboardSession(row rowScanner, hash string) (*protocolv1.DashboardSession, error) {
	var expires, absolute int64
	var remember int
	err := row.Scan(&expires, &absolute, &remember)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &protocolv1.DashboardSession{TokenHash: hash, ExpiresAt: timestamppb.New(time.Unix(expires, 0)), AbsoluteExpiresAt: timestamppb.New(time.Unix(absolute, 0)), RememberDevice: remember == 1}, nil
}

func dashboardRememberDeviceValue(session *protocolv1.DashboardSession) int {
	if session.GetRememberDevice() {
		return 1
	}
	return 0
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
