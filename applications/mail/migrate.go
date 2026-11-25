package mail

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/00001_imap.sql
var imapMigration string

func (p Plugin) Start(ctx context.Context) error {
	return pgx.BeginFunc(ctx, p.Module.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('horizon:mail:imap:' || current_schema(), 0))"); err != nil {
			return err
		}
		var installed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_attribute
			WHERE attrelid = 'mail_channels'::regclass AND attname = 'imap_uid_validity' AND NOT attisdropped
		)`).Scan(&installed); err != nil {
			return err
		}
		if installed {
			return nil
		}
		if _, err := tx.Exec(ctx, imapMigration); err != nil {
			return fmt.Errorf("upgrade mail receive channels: %w", err)
		}
		return nil
	})
}
