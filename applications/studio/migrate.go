package studio

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var Migrations embed.FS

func (p Plugin) Start(ctx context.Context) error {
	return pgx.BeginFunc(ctx, p.Module.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('horizon:studio:schema:' || current_schema(), 0))"); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS studio_schema_migrations (version text PRIMARY KEY)"); err != nil {
			return err
		}
		files, err := Migrations.ReadDir("migrations")
		if err != nil {
			return err
		}
		for _, file := range files {
			var applied bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM studio_schema_migrations WHERE version = $1)", file.Name()).Scan(&applied); err != nil {
				return err
			}
			if applied {
				continue
			}
			sql, err := Migrations.ReadFile("migrations/" + file.Name())
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("apply Studio migration %s: %w", file.Name(), err)
			}
			if _, err := tx.Exec(ctx, "INSERT INTO studio_schema_migrations (version) VALUES ($1)", file.Name()); err != nil {
				return err
			}
		}
		return nil
	})
}
