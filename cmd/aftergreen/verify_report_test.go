package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type reportDoc struct {
	Version           int    `json:"version"`
	AftergreenVersion string `json:"aftergreen_version"`
	Deployment        struct {
		Name     string `json:"name"`
		Revision string `json:"revision"`
	} `json:"deployment"`
	Verdict string `json:"verdict"`
	Summary struct {
		Verified, Failed, Unknown int
	} `json:"summary"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
	DurationMS  int64  `json:"duration_ms"`
	Checks      []struct {
		ID             string   `json:"id"`
		Type           string   `json:"type"`
		Verdict        string   `json:"verdict"`
		Reasons        []string `json:"reasons"`
		Attempts       int      `json:"attempts"`
		DurationMS     int64    `json:"duration_ms"`
		ObservedStatus int      `json:"observed_status"`
	} `json:"checks"`
}

func decodeReport(t *testing.T, b []byte) reportDoc {
	t.Helper()
	var r reportDoc
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		t.Fatalf("not a JSON report: %v\n%s", err, b)
	}
	if dec.More() {
		t.Fatalf("extra data after the report:\n%s", b)
	}
	return r
}

func TestVerifyJSONReport(t *testing.T) {
	srv := versionServer(t, "0ld0ld0")
	t.Setenv("DEPLOY_SHA", "a82f19c")
	t.Setenv("API_TOKEN", "tok-not-printed")
	cfg := writeConfig(t, twoChecks(srv.URL))
	reportPath := filepath.Join(t.TempDir(), "evidence.json")

	code, out, errOut := runCLI("verify", "-f", cfg, "--json", "--report", reportPath)
	if code != exitFailed {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	if errOut != "" {
		t.Errorf("stderr must be empty in JSON mode: %q", errOut)
	}
	r := decodeReport(t, []byte(out))
	if r.Version != 1 || r.AftergreenVersion != "dev" || r.Deployment.Name != "payment-api" || r.Deployment.Revision != "a82f19c" ||
		r.Verdict != "FAILED" || r.Summary.Verified != 1 || r.Summary.Failed != 1 || r.Summary.Unknown != 0 || len(r.Checks) != 2 {
		t.Fatalf("%+v", r)
	}
	c := r.Checks[1]
	if c.ID != "correct-version" || c.Type != "http" || c.Verdict != "FAILED" || c.Attempts != 1 || c.ObservedStatus != 200 ||
		len(c.Reasons) != 1 || c.Reasons[0] != "response body did not contain the expected text" {
		t.Fatalf("%+v", c)
	}
	if r.Checks[0].Reasons == nil {
		t.Error("reasons must be an array, not null")
	}
	start, err1 := time.Parse(time.RFC3339Nano, r.StartedAt)
	end, err2 := time.Parse(time.RFC3339Nano, r.CompletedAt)
	if err1 != nil || err2 != nil || end.Before(start) || !strings.HasSuffix(r.StartedAt, "Z") || r.DurationMS < 0 {
		t.Errorf("timestamps %q %q", r.StartedAt, r.CompletedAt)
	}
	// No secrets, URLs, response values or terminal formatting.
	for _, bad := range []string{"tok-not-printed", srv.URL, "127.0.0.1", "0ld0ld0", "\x1b[", "─", "AFTERGREEN"} {
		if strings.Contains(out, bad) {
			t.Errorf("JSON report contains %q", bad)
		}
	}
	// The evidence file is byte-identical to stdout.
	file, err := os.ReadFile(reportPath)
	if err != nil || string(file) != out {
		t.Fatalf("report file differs from stdout (%v)", err)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(reportPath), ".aftergreen-report-*")); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

func TestVerifyReportWithTextOutput(t *testing.T) {
	srv := versionServer(t, "a82f19c")
	t.Setenv("DEPLOY_SHA", "a82f19c")
	t.Setenv("API_TOKEN", "x")
	reportPath := filepath.Join(t.TempDir(), "evidence.json")
	os.WriteFile(reportPath, []byte("stale"), 0o644) // replaced, not appended to
	code, out, _ := runCLI("verify", "-f", writeConfig(t, twoChecks(srv.URL)), "--report", reportPath)
	if code != exitVerified || !strings.Contains(out, "Deployment verification VERIFIED\nEvery expected outcome was observed.\n") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	b, _ := os.ReadFile(reportPath)
	if r := decodeReport(t, b); r.Verdict != "VERIFIED" {
		t.Fatalf("%+v", r)
	}
}

func TestVerifyJSONNoProgressOnRetry(t *testing.T) {
	srv := rolloutServer(t, 1)
	t.Setenv("DEPLOY_SHA", "a82f19c")
	code, out, errOut := runCLI("verify", "-f", writeConfig(t, retryConfig(srv.URL, "{interval: 1s, deadline: 10s}")), "--json")
	if code != exitVerified || errOut != "" {
		t.Fatalf("exit %d stderr %q", code, errOut)
	}
	if r := decodeReport(t, []byte(out)); r.Checks[0].Attempts != 2 {
		t.Fatalf("%+v", r.Checks[0])
	}
}

func TestVerifyJSONInvalidConfigHasNoReport(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "evidence.json")
	code, out, errOut := runCLI("verify", "-f", writeConfig(t, "version: 7\n"), "--json", "--report", reportPath)
	if code != exitInvalid || out != "" || !strings.Contains(errOut, "unsupported version 7") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errOut)
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Error("report written for invalid configuration")
	}
}

func TestVerifyUnwritableReportFailsBeforeChecks(t *testing.T) {
	srv := versionServer(t, "a82f19c")
	cfg := "version: 1\ndeployment: {name: web}\nchecks:\n  - {id: up, type: http, url: '" + srv.URL + "/health'}\n"
	code, out, errOut := runCLI("verify", "-f", writeConfig(t, cfg), "--report", filepath.Join(t.TempDir(), "no", "such", "dir", "r.json"))
	if code != exitInvalid || out != "" || !strings.Contains(errOut, "cannot write report") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out, errOut)
	}
}

func TestVerifyUnknownMeaningIsExplicit(t *testing.T) {
	cfg := "version: 1\ndeployment: {name: web}\nchecks:\n  - {id: down, type: http, url: '" + unreachableURL(t) + "', timeout: 1s}\n"
	code, out, _ := runCLI("verify", "-f", writeConfig(t, cfg))
	if code != exitUnknown || !strings.Contains(out, "This is not a pass.") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}
