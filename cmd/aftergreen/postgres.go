package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/foldnine/aftergreen/internal/postgres"
)

// postgresURLEnv holds the connection string. It is read from the environment
// only, never from flags, so it does not leak into shell history or process lists.
const postgresURLEnv = "AFTERGREEN_POSTGRES_URL"

func runPostgres(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("postgres", flag.ContinueOnError)
	contractPath := f.String("contract", "", "path to a version-1 JSON contract (required)")
	key := f.String("key", "", "event key; overrides the contract's \"key\"")
	timeout := f.Duration("timeout", 10*time.Second, "timeout for connecting and querying, 1s..60s")
	jsonOut := f.Bool("json", false, "emit machine-readable JSON verdict")
	if !parseFlags(f, args, stderr) {
		return exitInvalid
	}
	if *contractPath == "" {
		fmt.Fprintln(stderr, "error: --contract is required")
		return exitInvalid
	}
	if *timeout < time.Second || *timeout > 60*time.Second {
		fmt.Fprintln(stderr, "error: --timeout must be between 1s and 60s")
		return exitInvalid
	}
	file, err := postgres.LoadFile(*contractPath)
	if err != nil {
		fmt.Fprintln(stderr, "error: invalid contract:", err)
		return exitInvalid
	}
	keySet := false
	f.Visit(func(fl *flag.Flag) { keySet = keySet || fl.Name == "key" })
	if keySet && strings.TrimSpace(*key) == "" {
		// An empty --key usually means an unset CI variable. Falling back to the
		// contract's key could verify a stale row, so refuse instead.
		fmt.Fprintln(stderr, "error: --key was given but is empty (is the event/deploy ID variable set?)")
		return exitInvalid
	}
	contract := file.Contract(*key)
	if _, _, err := contract.Statement(); err != nil {
		fmt.Fprintln(stderr, "error: invalid contract:", err)
		return exitInvalid
	}
	connConfig, err := postgresConfig()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitInvalid
	}
	verdict, err := verifyPostgres(context.Background(), connConfig, contract, *timeout)
	if err != nil {
		fmt.Fprintln(stderr, "error: invalid contract:", err)
		return exitInvalid
	}
	if *jsonOut {
		b, _ := json.MarshalIndent(verdict, "", "  ")
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprintf(stdout, "%s  PostgreSQL outcome (%s.%s)\n", verdict.Status, schemaOrDefault(contract.Schema), contract.Table)
		for _, reason := range verdict.Reasons {
			fmt.Fprintln(stdout, "  -", reason)
		}
	}
	return exitCode(verdict.Status)
}

// postgresConfig parses the connection string from AFTERGREEN_POSTGRES_URL.
// Errors never contain the connection string.
func postgresConfig() (*pgx.ConnConfig, error) {
	dsn := os.Getenv(postgresURLEnv)
	if dsn == "" {
		return nil, fmt.Errorf("set %s to a PostgreSQL connection string for a read-only role", postgresURLEnv)
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		// Never print the parse error: it may contain the connection string.
		return nil, fmt.Errorf("%s is not a valid PostgreSQL connection string", postgresURLEnv)
	}
	return config, nil
}

// verifyPostgres opens one connection, runs postgres.Verify within timeout and
// closes the connection. A non-nil error means the contract is invalid.
func verifyPostgres(ctx context.Context, connConfig *pgx.ConnConfig, contract postgres.Contract, timeout time.Duration) (postgres.Verdict, error) {
	config := connConfig.Copy()
	config.ConnectTimeout = timeout
	// Only application_name is sent at startup: connection poolers such as
	// PgBouncer reject unknown startup parameters. Read-only is enforced per
	// transaction (BEGIN READ ONLY) and should also be set on the role.
	config.RuntimeParams["application_name"] = "aftergreen"
	db := stdlib.OpenDB(*config)
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	verdict, err := postgres.Verify(ctx, db, contract)
	if err != nil {
		return verdict, err
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && verdict.Status == postgres.StatusUnknown {
		verdict.Reasons = append(verdict.Reasons, fmt.Sprintf("timeout of %s exceeded", timeout))
	}
	return verdict, nil
}

func schemaOrDefault(s string) string {
	if s == "" {
		return "public"
	}
	return s
}
