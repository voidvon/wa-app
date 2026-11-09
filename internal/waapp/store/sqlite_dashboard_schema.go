package store

import "context"

func (s *SQLiteStore) migrateDashboardSessions(ctx context.Context) error {
	// Legacy sessions have no fixed login deadline and require a fresh login.
	for _, column := range []struct{ name, statement string }{
		{"absolute_expires_at", `ALTER TABLE wa_dashboard_sessions ADD COLUMN absolute_expires_at INTEGER NOT NULL DEFAULT 0`},
		{"remember_device", `ALTER TABLE wa_dashboard_sessions ADD COLUMN remember_device INTEGER NOT NULL DEFAULT 0`},
	} {
		var count int
		if err := s.queries.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('wa_dashboard_sessions') WHERE name=?`, column.name).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := s.queries.ExecContext(ctx, column.statement); err != nil {
				return err
			}
		}
	}
	return nil
}
