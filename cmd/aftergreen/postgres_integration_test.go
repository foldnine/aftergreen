//go:build integration

package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// End-to-end through the CLI against real PostgreSQL (scripts/test-postgres.sh).
func TestPostgresCLIIntegration(t *testing.T) {
	adminURL := os.Getenv("AFTERGREEN_TEST_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("AFTERGREEN_TEST_POSTGRES_URL not set")
	}
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	b := make([]byte, 6)
	rand.Read(b)
	schema := "ag_cli_" + hex.EncodeToString(b)
	for _, s := range []string{
		`CREATE SCHEMA ` + schema,
		`CREATE TABLE ` + schema + `.deploys (deploy_id text, version text, state text)`,
		`INSERT INTO ` + schema + `.deploys VALUES ('run-1001', 'v1.4.2', 'live'), ('run-1002', 'v1.4.1', 'rolled-back-secret')`,
	} {
		if _, err := admin.ExecContext(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	defer admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)

	contract := filepath.Join(t.TempDir(), "deploy.json")
	os.WriteFile(contract, []byte(fmt.Sprintf(`{"version":1,"schema":%q,"table":"deploys","key_column":"deploy_id","key":"run-1001","expect":{"version":"v1.4.2","state":"live"}}`, schema)), 0o600)

	t.Setenv(postgresURLEnv, adminURL)
	if code, out, errOut := runCLI("postgres", "--contract", contract); code != exitVerified || !strings.HasPrefix(out, "VERIFIED") {
		t.Fatalf("verified: %d %s %s", code, out, errOut)
	}
	code, out, _ := runCLI("postgres", "--contract", contract, "--key", "run-1002", "--json")
	if code != exitFailed || !strings.Contains(out, "state: value mismatch") || strings.Contains(out, "rolled-back-secret") {
		t.Fatalf("failed: %d %s", code, out)
	}
	if code, _, _ := runCLI("postgres", "--contract", contract, "--key", "run-9999"); code != exitFailed {
		t.Fatalf("missing: %d", code)
	}

	t.Setenv(postgresURLEnv, "postgres://nobody:pw-not-printed@127.0.0.1:1/x?connect_timeout=2")
	code, out, errOut := runCLI("postgres", "--contract", contract, "--timeout", "3s")
	if code != exitUnknown || strings.Contains(out+errOut, "pw-not-printed") {
		t.Fatalf("unknown: %d %s %s", code, out, errOut)
	}
}

// aftergreen verify with HTTP and PostgreSQL checks against real PostgreSQL.
func TestVerifyPostgresIntegration(t *testing.T) {
	adminURL := os.Getenv("AFTERGREEN_TEST_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("AFTERGREEN_TEST_POSTGRES_URL not set")
	}
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	b := make([]byte, 6)
	rand.Read(b)
	schema := "ag_verify_" + hex.EncodeToString(b)
	for _, s := range []string{
		`CREATE SCHEMA ` + schema,
		`CREATE TABLE ` + schema + `.deploys (deploy_id text, state text)`,
		`INSERT INTO ` + schema + `.deploys VALUES ('run-1001', 'live'), ('run-0999', 'live'), ('run-1002', 'failed-secret')`,
	} {
		if _, err := admin.ExecContext(context.Background(), s); err != nil {
			t.Fatal(err)
		}
	}
	defer admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)

	srv := versionServer(t, "a82f19c")
	dir := t.TempDir()
	// The contract file carries a stale key on purpose: verify must ignore it.
	os.WriteFile(filepath.Join(dir, "deploy.json"), []byte(fmt.Sprintf(
		`{"version":1,"schema":%q,"table":"deploys","key_column":"deploy_id","key":"run-0999","expect":{"state":"live"}}`, schema)), 0o600)
	cfg := filepath.Join(dir, "aftergreen.yaml")
	os.WriteFile(cfg, []byte(`version: 1
deployment: {name: payment-api, revision: "${DEPLOY_SHA}"}
checks:
  - {id: version, type: http, url: "`+srv.URL+`/version", expect: {contains: "${DEPLOY_SHA}"}}
  - {id: release-recorded, type: postgres, contract: deploy.json, key: "${DEPLOY_ID}"}
`), 0o600)
	t.Setenv(postgresURLEnv, adminURL)
	t.Setenv("DEPLOY_SHA", "a82f19c")

	t.Setenv("DEPLOY_ID", "run-1001")
	if code, out, errOut := runCLI("verify", "-f", cfg); code != exitVerified {
		t.Fatalf("verified: %d %s %s", code, out, errOut)
	}
	t.Setenv("DEPLOY_ID", "run-1002")
	code, out, _ := runCLI("verify", "-f", cfg)
	if code != exitFailed || !strings.Contains(out, "state: value mismatch") || strings.Contains(out, "failed-secret") {
		t.Fatalf("mismatch: %d %s", code, out)
	}
	// A row for an older deploy exists, but not for this one: FAILED, no fallback.
	t.Setenv("DEPLOY_ID", "run-1003")
	if code, out, _ := runCLI("verify", "-f", cfg); code != exitFailed || !strings.Contains(out, "destination record missing") {
		t.Fatalf("missing: %d %s", code, out)
	}
	t.Setenv("DEPLOY_ID", "")
	if code, _, errOut := runCLI("verify", "-f", cfg); code != exitInvalid || !strings.Contains(errOut, "DEPLOY_ID is empty") {
		t.Fatalf("empty key: %d %s", code, errOut)
	}
}
