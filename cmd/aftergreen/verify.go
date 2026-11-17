package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	jsonOut := f.Bool("json", false, "print the JSON report on stdout instead of the text report (no progress lines)")
	reportPath := f.String("report", "", "also write the JSON report to this file")
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
	// Create the report file before observing anything, so an unwritable path
	// is invalid usage rather than a lost result after a long run.
	var report *pendingFile
	if *reportPath != "" {
		if report, err = createPending(*reportPath); err != nil {
			fmt.Fprintln(stderr, "error: cannot write report:", err)
			return exitInvalid
		}
		defer report.abort()
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
	opts := runner.Options{}
	if !*jsonOut {
		opts.Progress = func(a runner.Attempt) {
			// Progress goes to stderr so stdout stays the report.
			line := fmt.Sprintf("aftergreen: %s: attempt %d %s", a.CheckID, a.Number, a.Verdict)
			if len(a.Reasons) > 0 {
				line += " (" + strings.Join(a.Reasons, "; ") + ")"
			}
			if a.Next > 0 {
				line += fmt.Sprintf(", retrying in %s", a.Next)
			}
			fmt.Fprintln(stderr, line)
		}
	}
	rep, err := runner.Run(ctx, cfg, engines, opts)
	if err != nil {
		fmt.Fprintln(stderr, "error: invalid configuration:", err)
		return exitInvalid
	}
	doc, err := rep.JSON(version)
	if err != nil {
		fmt.Fprintln(stderr, "error: cannot encode report:", err)
		return exitInvalid
	}
	if *jsonOut {
		if _, err := stdout.Write(doc); err != nil {
			fmt.Fprintf(stderr, "error: verification finished (%s) but the report could not be printed: %v\n", rep.Verdict, err)
			return exitInvalid
		}
	} else {
		writeText(stdout, rep)
	}
	if report != nil {
		if err := report.commit(doc); err != nil {
			fmt.Fprintf(stderr, "error: verification finished (%s) but the report could not be written: %v\n", rep.Verdict, err)
			return exitInvalid
		}
	}

	// The exit code follows the printed overall verdict, never a recount.
	return exitCode(rep.Verdict)
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
		attempts := ""
		if c.Attempts > 1 {
			attempts = fmt.Sprintf(" (%d attempts)", c.Attempts)
		}
		fmt.Fprintf(w, "%-8s  %s%s\n", c.Verdict, c.ID, attempts)
		for _, reason := range c.Reasons {
			fmt.Fprintf(w, "%-8s    - %s\n", "", reason)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, rule)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Deployment verification %s\n", rep.Verdict)
	fmt.Fprintf(w, "%s\n\n", verdictMeaning[rep.Verdict])
	verified, failed, unknown := rep.Counts()
	fmt.Fprintf(w, "%d verified\n%d failed\n%d unknown\n\n", verified, failed, unknown)
	fmt.Fprintf(w, "Duration: %s\n", formatDuration(rep.Completed.Sub(rep.Started)))
}

var verdictMeaning = map[string]string{
	runner.Verified: "Every expected outcome was observed.",
	runner.Failed:   "At least one expected outcome was observed to be wrong.",
	runner.Unknown:  "No outcome was wrong, but at least one could not be observed. This is not a pass.",
}

// pendingFile is written to a temporary file next to the target and renamed
// into place, so readers never see a partial report.
type pendingFile struct {
	tmp    *os.File
	target string
}

func createPending(target string) (*pendingFile, error) {
	if fi, err := os.Stat(target); err == nil && !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s exists and is not a regular file", target)
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".aftergreen-report-*")
	if err != nil {
		return nil, err
	}
	return &pendingFile{tmp: tmp, target: target}, nil
}

func (p *pendingFile) commit(b []byte) error {
	_, err := p.tmp.Write(b)
	if cerr := p.tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(p.tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(p.tmp.Name(), p.target)
	}
	if err == nil {
		p.tmp = nil
	}
	return err
}

// abort removes the temporary file unless commit succeeded.
func (p *pendingFile) abort() {
	if p.tmp != nil {
		p.tmp.Close()
		os.Remove(p.tmp.Name())
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
