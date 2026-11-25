//go:build integration && iamdrill

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"horizon/deployment/database"
	"horizon/foundation/system/identity"
)

// This opt-in drill requires pg_dump/pg_restore matching the PostgreSQL server,
// or IAM_DRILL_CONTAINER naming its local Docker container. It deliberately
// restores only iamFixture's unique schema, never the supplied whole database.
func TestIAMBackupRestoreDrill(t *testing.T) {
	handler, pool, account := iamFixture(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	config := pool.Config().ConnConfig
	if ip := net.ParseIP(config.Host); ip == nil || !ip.IsLoopback() {
		t.Fatal("backup drill requires an explicit local loopback TEST_DATABASE_URL")
	}
	loginIAM(t, handler, account.Username)
	var schema string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil || !regexp.MustCompile(`^iam_[0-9]+$`).MatchString(schema) {
		t.Fatal("unexpected fixture schema", err)
	}
	before := iamRestoreFacts(t, ctx, pool, schema)
	archive := iamPostgresClient(t, ctx, pool, "pg_dump", nil,
		"--format=custom", "--no-owner", "--no-privileges", "--strict-names", "--schema="+schema)
	if len(archive) == 0 {
		t.Fatal("empty PostgreSQL backup")
	}
	pool.Reset()
	if _, err := pool.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
		t.Fatal(err)
	}
	iamPostgresClient(t, ctx, pool, "pg_restore", archive, "--exit-on-error", "--no-owner", "--no-privileges")
	pool.Reset()
	if !reflect.DeepEqual(before, iamRestoreFacts(t, ctx, pool, schema)) {
		t.Fatal("backup restore changed table data or sequence state")
	}
	if initialized, err := database.Initialize(ctx, config.ConnString(), database.Options{}); err != nil || initialized {
		t.Fatalf("initialization must recognize the restored baseline: initialized=%t (%v)", initialized, err)
	}
	principal, err := identity.New(pool, false).PrincipalForTask(ctx, "tenant-a", account.ID)
	if err != nil || !principal.Can("access:manage") {
		t.Fatal("restored formal administrator authority unavailable", err)
	}
	loginIAM(t, handler, account.Username)
	t.Logf("restored %d table/sequence snapshots from a fresh schema and verified bootstrap administrator login", len(before))
}

func iamPostgresClient(t *testing.T, ctx context.Context, pool *pgxpool.Pool, program string, input []byte, args ...string) []byte {
	t.Helper()
	config := pool.Config().ConnConfig
	// pg_restore requires an explicit target even when PGDATABASE is set.
	args = append([]string{"--dbname=" + config.Database}, args...)
	var command *exec.Cmd
	container := os.Getenv("IAM_DRILL_CONTAINER")
	env := []string{
		"PGHOST=" + config.Host, "PGPORT=" + strconv.Itoa(int(config.Port)),
		"PGDATABASE=" + config.Database, "PGUSER=" + config.User, "PGPASSWORD=" + config.Password,
		"PGSSLMODE=disable", "PGCONNECT_TIMEOUT=10",
	}
	if container != "" {
		if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`).MatchString(container) {
			t.Fatal("invalid IAM_DRILL_CONTAINER")
		}
		invocation := []string{"exec", "-i"}
		// Docker forwards named client environment values; the password is never
		// present in command arguments or diagnostic output.
		for _, setting := range env {
			key, _, _ := strings.Cut(setting, "=")
			invocation = append(invocation, "-e", key)
		}
		env[0], env[1] = "PGHOST=127.0.0.1", "PGPORT=5432"
		invocation = append(invocation, container, program)
		command = exec.CommandContext(ctx, "docker", append(invocation, args...)...)
	} else {
		command = exec.CommandContext(ctx, program, args...)
	}
	for _, setting := range os.Environ() {
		if !strings.HasPrefix(setting, "PG") {
			command.Env = append(command.Env, setting)
		}
	}
	command.Env = append(command.Env, env...)
	command.Stdin = bytes.NewReader(input)
	var output, diagnostic bytes.Buffer
	command.Stdout, command.Stderr = &output, &diagnostic
	if err := command.Run(); err != nil {
		message := diagnostic.String()
		if config.Password != "" {
			message = strings.ReplaceAll(message, config.Password, "[redacted]")
		}
		t.Fatalf("%s failed: %v: %s", program, err, message)
	}
	return output.Bytes()
}

func iamRestoreFacts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, schema string) map[string]string {
	t.Helper()
	result := make(map[string]string)
	names := func(query string) []string {
		t.Helper()
		rows, err := pool.Query(ctx, query, schema)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var values []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatal(err)
			}
			values = append(values, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return values
	}
	for _, name := range names(`SELECT table_name FROM information_schema.tables
		WHERE table_schema=$1 AND table_type='BASE TABLE' ORDER BY table_name`) {
		var value string
		if err := pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text
			FROM `+pgx.Identifier{schema, name}.Sanitize()+` t`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		result["table:"+name] = value
	}
	// information_schema.sequences omits identity-column sequences. pg_sequences
	// includes them, so GENERATED ALWAYS AS IDENTITY is covered by the drill.
	for _, name := range names(`SELECT sequencename FROM pg_sequences WHERE schemaname=$1 ORDER BY sequencename`) {
		var value int64
		var called bool
		if err := pool.QueryRow(ctx, `SELECT last_value,is_called FROM `+pgx.Identifier{schema, name}.Sanitize()).Scan(&value, &called); err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal([]any{value, called})
		if err != nil {
			t.Fatal(err)
		}
		result["sequence:"+name] = string(encoded)
	}
	return result
}
