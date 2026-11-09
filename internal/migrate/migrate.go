package migrate

import (
	"context"
	"fmt"
	"os"

	"github.com/pood1e/wa-app/internal/waapp/store"
)

func Run(ctx context.Context) error {
	switch os.Getenv("WA_APP_STORE") {
	case "sqlite":
		path := os.Getenv("WA_APP_SQLITE_FILE")
		if path == "" {
			return fmt.Errorf("WA_APP_SQLITE_FILE is required")
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("existing SQLite database is required")
		}
		return store.MigrateLegacySQLite(ctx, path)
	case "postgres":
		dsn := os.Getenv("WA_APP_PG_DSN")
		if dsn == "" {
			return fmt.Errorf("WA_APP_PG_DSN is required")
		}
		return store.MigrateLegacyPostgres(ctx, dsn)
	default:
		return fmt.Errorf("WA_APP_STORE must be postgres or sqlite")
	}
}
