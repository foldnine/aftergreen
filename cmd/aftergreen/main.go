// Command aftergreen verifies that an automation's claimed success matches an
// observable outcome. Exit codes: 0 verified, 1 failed, 2 invalid input or
// usage, 3 unknown (no failures, but at least one outcome unobservable).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/foldnine/aftergreen/internal/verify"
)

const (
	exitVerified = 0
	exitFailed   = 1
	exitInvalid  = 2
	exitUnknown  = 3
)

// version is set at build time: -ldflags "-X main.version=v0.1.0".
var version = "dev"

const demoJSON = `{
  "events": [
    {"id":"daily-import-001","execution_status":"SUCCESS","observation_available":true,"expected":{"count":"20"},"actual":{"count":"20"}},
    {"id":"lead-sync-182","execution_status":"SUCCESS","observation_available":true,"expected":{"email":"john@example.com","status":"qualified"},"actual":{"email":"","status":"qualified"}},
    {"id":"report-003","execution_status":"SUCCESS","observation_available":false,"expected":{"report":"delivered"},"actual":{}}
  ]
}`

const usageText = `Usage:
  aftergreen verify [-f aftergreen.yaml]
                     (verify every check in a contract file; see docs/verify.md)
  aftergreen demo  [--dir PATH]
  aftergreen check [--file PATH] [--json]
  aftergreen http  --url URL [--status 200] [--contains TEXT] [--timeout 10s] [--json]
  aftergreen postgres --contract FILE [--key KEY] [--timeout 10s] [--json]
                     (connection string from AFTERGREEN_POSTGRES_URL)
  aftergreen version

Exit codes: 0 verified, 1 failed, 2 invalid input, 3 unknown.`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usageText)
		return exitInvalid
	}
	switch args[0] {
	case "demo":
		return runDemo(args[1:], stdout, stderr)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "http":
		return runHTTP(args[1:], stdout, stderr)
	case "postgres":
		return runPostgres(args[1:], stdout, stderr)
	case "version", "--version":
		fmt.Fprintln(stdout, "aftergreen", version)
		return exitVerified
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, usageText)
		return exitVerified
	default:
		fmt.Fprintf(stderr, "error: unknown command %q\n%s\n", args[0], usageText)
		return exitInvalid
	}
}

// parseFlags parses a subcommand's flags and rejects positional arguments,
// which are almost always a mistyped flag.
func parseFlags(f *flag.FlagSet, args []string, stderr io.Writer) bool {
	f.SetOutput(stderr)
	if err := f.Parse(args); err != nil {
		return false
	}
	if f.NArg() > 0 {
		fmt.Fprintf(stderr, "error: unexpected argument %q\n", f.Arg(0))
		return false
	}
	return true
}

func runDemo(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("demo", flag.ContinueOnError)
	dir := f.String("dir", ".", "directory for demo evidence")
	if !parseFlags(f, args, stderr) {
		return exitInvalid
	}
	path := filepath.Join(*dir, "aftergreen-demo.json")
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitInvalid
	}
	h, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		fmt.Fprintln(stderr, "error: cannot create demo evidence (existing files are never overwritten):", err)
		return exitInvalid
	}
	_, err = h.WriteString(demoJSON + "\n")
	if cerr := h.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return exitInvalid
	}
	fmt.Fprintln(stdout, "Demo evidence written to", path)
	fmt.Fprintf(stdout, "Run: aftergreen check --file %s\n", path)
	return exitVerified
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("check", flag.ContinueOnError)
	path := f.String("file", "aftergreen-demo.json", "path to evidence JSON")
	jsonOut := f.Bool("json", false, "emit machine-readable JSON")
	if !parseFlags(f, args, stderr) {
		return exitInvalid
	}
	in, err := verify.Read(*path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "error: %s not found (create one with: aftergreen demo)\n", *path)
		} else {
			fmt.Fprintln(stderr, "error:", err)
		}
		return exitInvalid
	}
	results := verify.Evaluate(in)
	if *jsonOut {
		b, _ := json.MarshalIndent(results, "", "  ")
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprintln(stdout, "AFTERGREEN — Outcome verification")
		for _, r := range results {
			fmt.Fprintf(stdout, "\n%s  %s (workflow reported: %s)\n", r.Status, r.ID, r.ExecutionStatus)
			for _, reason := range r.Reasons {
				fmt.Fprintln(stdout, "  -", reason)
			}
		}
	}
	statuses := make([]string, len(results))
	for i, r := range results {
		statuses[i] = r.Status
	}
	return exitCode(statuses...)
}

// exitCode combines verdicts: any FAILED wins, then any UNKNOWN.
func exitCode(statuses ...string) int {
	code := exitVerified
	for _, s := range statuses {
		switch s {
		case "FAILED":
			return exitFailed
		case "VERIFIED":
		default:
			code = exitUnknown
		}
	}
	return code
}
