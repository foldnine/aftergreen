//go:build integration

package postgres

// Real-PostgreSQL integration tests. Run with:
//
//	scripts/test-postgres.sh            # starts a disposable container
//	AFTERGREEN_TEST_POSTGRES_URL=postgres://... go test -tags integration ./...
//
// The URL must point to a disposable database and a role that may create
// roles and schemas. Every run uses its own schema and reader role and drops
// them afterwards.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type fixture struct {
	admin     *sql.DB
	schema    string
	readerURL string
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func openURL(t *testing.T, raw string) *sql.DB {
	t.Helper()
	cfg, err := pgx.ParseConfig(raw)
	if err != nil {
		t.Fatal("parse test URL")
	}
	cfg.ConnectTimeout = 5 * time.Second
	db := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { db.Close() })
	return db
}

func setup(t *testing.T) fixture {
	t.Helper()
	adminURL := os.Getenv("AFTERGREEN_TEST_POSTGRES_URL")
	if adminURL == "" {
		t.Skip("AFTERGREEN_TEST_POSTGRES_URL not set; see scripts/test-postgres.sh")
	}
	admin := openURL(t, adminURL)
	suffix := randomHex(t, 6)
	schema, role, password := "ag_it_"+suffix, "ag_reader_"+suffix, randomHex(t, 16)
	ctx := context.Background()
	stmts := []string{
		fmt.Sprintf(`CREATE SCHEMA %s`, schema),
		fmt.Sprintf(`CREATE TABLE %s.leads (event_id text, status text, amount numeric(10,2), qualified boolean, note text)`, schema),
		fmt.Sprintf(`CREATE TABLE %s.orders (id bigint PRIMARY KEY, state text)`, schema),
		fmt.Sprintf(`CREATE TABLE %s.private_notes (event_id text, body text)`, schema),
		fmt.Sprintf(`INSERT INTO %s.leads VALUES
			('evt-ok', 'qualified', 120.50, true, 'synthetic'),
			('evt-mismatch', 'pending', 120.50, true, 'synthetic'),
			('evt-null', NULL, 120.50, true, 'synthetic'),
			('evt-dup', 'qualified', 1.00, true, 'a'),
			('evt-dup', 'qualified', 1.00, true, 'b')`, schema),
		fmt.Sprintf(`INSERT INTO %s.orders VALUES (42, 'shipped')`, schema),
		fmt.Sprintf(`INSERT INTO %s.private_notes VALUES ('evt-ok', 'hidden')`, schema),
		fmt.Sprintf(`CREATE ROLE %s LOGIN PASSWORD '%s' NOSUPERUSER NOCREATEDB NOCREATEROLE`, role, password),
		fmt.Sprintf(`GRANT USAGE ON SCHEMA %s TO %s`, schema, role),
		fmt.Sprintf(`GRANT SELECT ON %s.leads, %s.orders TO %s`, schema, schema, role),
	}
	for _, s := range stmts {
		if _, err := admin.ExecContext(ctx, s); err != nil {
			t.Fatalf("setup %q: %v", strings.SplitN(s, " ", 3)[:2], err)
		}
	}
	t.Cleanup(func() {
		admin.Exec(fmt.Sprintf(`DROP SCHEMA %s CASCADE`, schema))
		admin.Exec(fmt.Sprintf(`DROP ROLE %s`, role))
	})
	u, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal("test URL must be a postgres:// URL")
	}
	u.User = url.UserPassword(role, password)
	return fixture{admin: admin, schema: schema, readerURL: u.String()}
}

func TestIntegrationOutcomes(t *testing.T) {
	fx := setup(t)
	reader := openURL(t, fx.readerURL)
	lead := func(key string, expect map[string]string) Contract {
		return Contract{Schema: fx.schema, Table: "leads", KeyColumn: "event_id", KeyValue: key, Expected: expect}
	}
	cases := []struct {
		name     string
		contract Contract
		want     string
		reason   string
	}{
		{"verified", lead("evt-ok", map[string]string{"status": "qualified", "amount": "120.50", "qualified": "true"}), StatusVerified, ""},
		{"mismatch", lead("evt-mismatch", map[string]string{"status": "qualified"}), StatusFailed, "status: value mismatch"},
		{"numeric canonical text", lead("evt-ok", map[string]string{"amount": "120.5"}), StatusFailed, "amount: value mismatch"},
		{"missing", lead("evt-nope", map[string]string{"status": "qualified"}), StatusFailed, "destination record missing"},
		{"duplicate", lead("evt-dup", map[string]string{"status": "qualified"}), StatusFailed, "multiple destination rows"},
		{"null", lead("evt-null", map[string]string{"status": "qualified"}), StatusFailed, "status: NULL"},
		{"integer key", Contract{Schema: fx.schema, Table: "orders", KeyColumn: "id", KeyValue: "42", Expected: map[string]string{"state": "shipped"}}, StatusVerified, ""},
		{"key not castable", Contract{Schema: fx.schema, Table: "orders", KeyColumn: "id", KeyValue: "not-a-number", Expected: map[string]string{"state": "shipped"}}, StatusUnknown, ""},
		{"permission denied", Contract{Schema: fx.schema, Table: "private_notes", KeyColumn: "event_id", KeyValue: "evt-ok", Expected: map[string]string{"body": "hidden"}}, StatusUnknown, "SQLSTATE 42501"},
		{"missing table", Contract{Schema: fx.schema, Table: "nope", KeyColumn: "event_id", KeyValue: "evt-ok", Expected: map[string]string{"x": "1"}}, StatusUnknown, "SQLSTATE 42P01"},
		{"missing column", lead("evt-ok", map[string]string{"no_such_column": "x"}), StatusUnknown, "SQLSTATE 42703"},
		{"case-sensitive identifiers", Contract{Schema: fx.schema, Table: "Leads", KeyColumn: "event_id", KeyValue: "evt-ok", Expected: map[string]string{"status": "qualified"}}, StatusUnknown, "SQLSTATE 42P01"},
		{"injection is just a key value", lead("evt-ok' OR '1'='1", map[string]string{"status": "qualified"}), StatusFailed, "destination record missing"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got, err := Verify(ctx, reader, tc.contract)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want {
				t.Fatalf("got %+v, want %s", got, tc.want)
			}
			joined := strings.Join(got.Reasons, "; ")
			if tc.reason != "" && !strings.Contains(joined, tc.reason) {
				t.Fatalf("reasons %q do not contain %q", joined, tc.reason)
			}
			for _, leak := range []string{"pending", "hidden", "synthetic"} {
				if strings.Contains(joined, leak) {
					t.Fatalf("reason leaks row content %q: %s", leak, joined)
				}
			}
		})
	}
}

func TestIntegrationTransactionIsReadOnly(t *testing.T) {
	fx := setup(t)
	reader := openURL(t, fx.readerURL)
	tx, err := reader.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var ro string
	if err := tx.QueryRow(`SHOW transaction_read_only`).Scan(&ro); err != nil || ro != "on" {
		t.Fatalf("transaction_read_only=%q err=%v", ro, err)
	}
}

func TestIntegrationLockTimeoutIsUnknown(t *testing.T) {
	fx := setup(t)
	reader := openURL(t, fx.readerURL)
	lock, err := fx.admin.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	if _, err := lock.Exec(fmt.Sprintf(`LOCK TABLE %s.leads IN ACCESS EXCLUSIVE MODE`, fx.schema)); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	start := time.Now()
	got, err := Verify(ctx, reader, Contract{Schema: fx.schema, Table: "leads", KeyColumn: "event_id", KeyValue: "evt-ok", Expected: map[string]string{"status": "qualified"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusUnknown || time.Since(start) > 4*time.Second {
		t.Fatalf("got %+v after %s", got, time.Since(start))
	}
}

func TestIntegrationConnectionFailuresAreUnknown(t *testing.T) {
	fx := setup(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := l.Addr().(*net.TCPAddr).Port
	l.Close()
	u, _ := url.Parse(fx.readerURL)
	refused := *u
	refused.Host = fmt.Sprintf("127.0.0.1:%d", closedPort)
	badPassword := *u
	badPassword.User = url.UserPassword(u.User.Username(), "wrong-password")
	for name, raw := range map[string]string{"refused": refused.String(), "bad password": badPassword.String()} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got, err := Verify(ctx, openURL(t, raw), Contract{Schema: fx.schema, Table: "leads", KeyColumn: "event_id", KeyValue: "evt-ok", Expected: map[string]string{"status": "qualified"}})
			if err != nil || got.Status != StatusUnknown {
				t.Fatalf("got %+v, %v", got, err)
			}
			if strings.Contains(strings.Join(got.Reasons, " "), "wrong-password") {
				t.Fatal("reason leaks password")
			}
		})
	}
}
