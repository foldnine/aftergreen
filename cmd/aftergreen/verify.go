package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/foldnine/aftergreen/internal/config"
	"github.com/foldnine/aftergreen/internal/httpcheck"
	"github.com/foldnine/aftergreen/internal/postgres"
	"github.com/foldnine/aftergreen/internal/runner"
)

const rule = "─────────────────────────────────────"

// lookupEnv is replaced in tests.
var lookupEnv config.LookupFunc = os.LookupEnv

func runVerify(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("verify", flag.ContinueOnError)
	path := "aftergreen.yaml"
	f.StringVar(&path, "f", path, "path to the verification contract")
	f.StringVar(&path, "file", path, "same as -f")
	if !parseFlags(f, args, stderr) {
		return exitInvalid
	}

	cfg, err := config.Load(path, lookupEnv)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "error: %s not found (see examples/verification/aftergreen.yaml)\n", path)
		} else {
			fmt.Fprintf(stderr, "error: invalid configuration %s: %v\n", path, err)
		}
		return exitInvalid
	}

	var connConfig *pgx.ConnConfig
	if cfg.UsesPostgres {
		if connConfig, err = postgresConfig(); err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return exitInvalid
		}
	}
	httpcheck.UserAgent = "aftergreen/" + version
	engines := runner.Engines{
		HTTP: httpcheck.Verify,
		Postgres: func(ctx context.Context, c postgres.Contract, timeout time.Duration) (postgres.Verdict, error) {
			return verifyPostgres(ctx, connConfig, c, timeout)
		},
	}

	// Ctrl-C or SIGTERM (a cancelled CI job) stops the run; unfinished checks
	// are reported UNKNOWN rather than silently dropped.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rep, err := runner.Run(ctx, cfg, engines, time.Now)
	if err != nil {
		fmt.Fprintln(stderr, "error: invalid configuration:", err)
		return exitInvalid
	}
	writeText(stdout, rep)

	verdicts := make([]string, len(rep.Checks))
	for i, c := range rep.Checks {
		verdicts[i] = c.Verdict
	}
	return exitCode(verdicts...)
}

func writeText(w io.Writer, rep runner.Report) {
	fmt.Fprintln(w, "AFTERGREEN")
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Deployment: %s\n", rep.Deployment.Name)
	if rep.Deployment.Revision != "" {
		fmt.Fprintf(w, "Revision:   %s\n", rep.Deployment.Revision)
	}
	fmt.Fprintln(w)
	for _, c := range rep.Checks {
		fmt.Fprintf(w, "%-8s  %s\n", c.Verdict, c.ID)
		for _, reason := range c.Reasons {
			fmt.Fprintf(w, "%-8s    - %s\n", "", reason)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Deployment verification %s\n\n", rep.Verdict)
	verified, failed, unknown := rep.Counts()
	fmt.Fprintf(w, "%d verified\n%d failed\n%d unknown\n\n", verified, failed, unknown)
	fmt.Fprintf(w, "Duration: %s\n", formatDuration(rep.Completed.Sub(rep.Started)))
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
