package database

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

//go:embed init.sql
var Files embed.FS

type Options struct {
	Tenant            string
	Username          string
	PasswordHash      string
	SkipAdministrator bool
}

// Initialize installs the deployment baseline once in the connection's current
// schema. Schema creation and administrator provisioning commit together.
func Initialize(ctx context.Context, url string, options Options) (bool, error) {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return false, err
	}
	defer conn.Close(ctx)

	var initialized bool
	err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('horizon:init:' || current_schema(), 0))"); err != nil {
			return err
		}
		var baselineExists, schemaInUse bool
		if err := tx.QueryRow(ctx, `
			SELECT to_regclass(format('%I.horizon_database_initialized', current_schema())) IS NOT NULL,
			       to_regclass(format('%I.iam_accounts', current_schema())) IS NOT NULL
		`).Scan(&baselineExists, &schemaInUse); err != nil {
			return err
		}
		if baselineExists {
			return nil
		}
		if schemaInUse {
			return errors.New("Horizon initialization requires an empty database schema")
		}

		skipAdministrator := "off"
		if options.SkipAdministrator {
			skipAdministrator = "on"
		}
		if _, err := tx.Exec(ctx, `
			SELECT set_config('horizon.bootstrap_tenant', $1, true),
			       set_config('horizon.bootstrap_username', $2, true),
			       set_config('horizon.bootstrap_password_hash', $3, true),
			       set_config('horizon.bootstrap_skip_administrator', $4, true)
		`, options.Tenant, options.Username, options.PasswordHash, skipAdministrator); err != nil {
			return err
		}
		baseline, err := Files.ReadFile("init.sql")
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(baseline)); err != nil {
			return fmt.Errorf("apply initialization SQL: %w", err)
		}
		initialized = true
		return nil
	})
	return initialized && err == nil, err
}
