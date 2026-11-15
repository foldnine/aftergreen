package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	contract := file.Contract(*key)
	if _, _, err := contract.Statement(); err != nil {
		fmt.Fprintln(stderr, "error: invalid contract:", err)
		return exitInvalid
	}
	dsn := os.Getenv(postgresURLEnv)
	if dsn == "" {
		fmt.Fprintf(stderr, "error: set %s to a PostgreSQL connection string for a read-only role\n", postgresURLEnv)
		return exitInvalid
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		// Never print the parse error: it may contain the connection string.
		fmt.Fprintf(stderr, "error: %s is not a valid PostgreSQL connection string\n", postgresURLEnv)
		return exitInvalid
	}
	config.ConnectTimeout = *timeout
	config.RuntimeParams["application_name"] = "aftergreen"
	config.RuntimeParams["default_transaction_read_only"] = "on"
	db := stdlib.OpenDB(*config)
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	verdict, err := postgres.Verify(ctx, db, contract)
	if err != nil {
		fmt.Fprintln(stderr, "error: invalid contract:", err)
		return exitInvalid
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) && verdict.Status == postgres.StatusUnknown {
		verdict.Reasons = append(verdict.Reasons, fmt.Sprintf("timeout of %s exceeded", *timeout))
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

func schemaOrDefault(s string) string {
	if s == "" {
		return "public"
	}
	return s
}
