package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/foldnine/aftergreen/internal/httpcheck"
)

func runHTTP(args []string, stdout, stderr io.Writer) int {
	f := flag.NewFlagSet("http", flag.ContinueOnError)
	endpoint := f.String("url", "", "maintainer-controlled http(s) URL to verify (required)")
	expectedStatus := f.Int("status", 200, "expected HTTP status (redirects are not followed)")
	bodyContains := f.String("contains", "", "literal text required in the response body (optional)")
	timeout := f.Duration("timeout", 10*time.Second, "timeout for the whole request, 1s..60s")
	jsonOutput := f.Bool("json", false, "emit machine-readable JSON verdict")
	if !parseFlags(f, args, stderr) {
		return exitInvalid
	}
	httpcheck.UserAgent = "aftergreen/" + version
	verdict, err := httpcheck.Verify(context.Background(), httpcheck.Contract{
		URL: *endpoint, ExpectedStatus: *expectedStatus, ExpectedBodySubstring: *bodyContains, Timeout: *timeout,
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
