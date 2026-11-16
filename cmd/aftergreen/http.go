package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/foldnine/aftergreen/internal/httpcheck"
)

func runHTTP(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("http", flag.ContinueOnError)
	endpoint := f.String("url", "", "maintainer-controlled http(s) URL to verify (required)")
	statusFlag := f.String("status", "200", "expected HTTP status, 100..599 (redirects are not followed)")
	bodyContains := f.String("contains", "", "literal text required in the response body (optional)")
	timeout := f.Duration("timeout", 10*time.Second, "timeout for the whole request, 1s..60s")
	jsonOutput := f.Bool("json", false, "emit machine-readable JSON verdict")
	if !parseFlags(f, args, stderr) {
		return exitInvalid
	}
	// Parsed as plain decimal: flag.Int would read "0200" as octal (128).
	expectedStatus, err := strconv.Atoi(strings.TrimSpace(*statusFlag))
	if err != nil || strings.TrimSpace(*statusFlag) != strconv.Itoa(expectedStatus) {
		fmt.Fprintln(stderr, "error: invalid HTTP check: --status must be a decimal number between 100 and 599")
		return exitInvalid
	}
	httpcheck.UserAgent = "aftergreen/" + version
	verdict, err := httpcheck.Verify(context.Background(), httpcheck.Contract{
		URL: *endpoint, ExpectedStatus: expectedStatus, ExpectedBodySubstring: *bodyContains, Timeout: *timeout,
	})
	if err != nil {
		fmt.Fprintln(stderr, "error: invalid HTTP check:", err)
		return exitInvalid
	}
	if *jsonOutput {
		fmt.Fprintln(stdout, verdict.JSON())
	} else {
		observed := "no response"
		if verdict.ObservedStatus != 0 {
			observed = fmt.Sprintf("HTTP %d", verdict.ObservedStatus)
		}
		fmt.Fprintf(stdout, "%s  HTTP outcome (%s)\n", verdict.Status, observed)
		for _, reason := range verdict.Reasons {
			fmt.Fprintln(stdout, "  -", reason)
		}
	}
	return exitCode(verdict.Status)
}
