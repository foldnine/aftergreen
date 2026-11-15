package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestExitCode(t *testing.T) {
	cases := []struct {
		in   []string
		want int
	}{
		{[]string{"VERIFIED"}, 0},
		{[]string{"VERIFIED", "UNKNOWN"}, 3},
		{[]string{"UNKNOWN", "FAILED"}, 1},
		{[]string{"weird"}, 3},
		{nil, 0},
	}
	for _, c := range cases {
		if got := exitCode(c.in...); got != c.want {
			t.Errorf("exitCode(%v)=%d want %d", c.in, got, c.want)
		}
	}
}

func TestDemoThenCheckFails(t *testing.T) {
	dir := t.TempDir()
	if code, _, stderr := runCLI("demo", "--dir", dir); code != 0 {
		t.Fatalf("demo: %d %s", code, stderr)
	}
	if code, _, _ := runCLI("demo", "--dir", dir); code != exitInvalid {
		t.Fatalf("demo overwrite should be refused, got %d", code)
	}
	code, stdout, _ := runCLI("check", "--file", filepath.Join(dir, "aftergreen-demo.json"))
	if code != exitFailed || !strings.Contains(stdout, "FAILED  lead-sync-182") || !strings.Contains(stdout, "UNKNOWN  report-003") {
		t.Fatalf("check: %d\n%s", code, stdout)
	}
}

func TestCheckUnknownOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "e.json")
	os.WriteFile(p, []byte(`{"events":[{"id":"a","observation_available":false,"expected":{"x":"1"}}]}`), 0o600)
	if code, _, _ := runCLI("check", "--file", p, "--json"); code != exitUnknown {
		t.Fatalf("got %d", code)
	}
}

func TestInvalidUsage(t *testing.T) {
	for _, args := range [][]string{
		{}, {"nope"}, {"check", "--file", "/does/not/exist.json"}, {"check", "stray"},
		{"http"}, {"http", "--url", "https://u:p@example.invalid/"}, {"http", "--url", "http://x.invalid", "--timeout", "0s"},
		{"http", "--url", "http://x.invalid", "--status", "abc"},
		{"http", "--url", "http://x.invalid", "--status", "0200"}, {"http", "--url", "http://x.invalid", "--status", "0x0C8"},
	} {
		if code, _, _ := runCLI(args...); code != exitInvalid {
			t.Errorf("%v: got %d want 2", args, code)
		}
	}
}

func TestHTTPExitCodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("build=abc")) }))
	defer srv.Close()
	if code, out, _ := runCLI("http", "--url", srv.URL, "--contains", "build=abc"); code != 0 || !strings.HasPrefix(out, "VERIFIED") {
		t.Fatalf("verified: %d %s", code, out)
	}
	if code, out, _ := runCLI("http", "--url", srv.URL, "--contains", "build=zzz", "--json"); code != 1 || strings.Contains(out, "zzz") {
		t.Fatalf("failed: %d %s", code, out)
	}
	if code, out, _ := runCLI("http", "--url", "http://127.0.0.1:1/", "--timeout", "1s"); code != 3 || !strings.Contains(out, "no response") {
		t.Fatalf("unknown: %d %s", code, out)
	}
}

func TestVersion(t *testing.T) {
	if code, out, _ := runCLI("version"); code != 0 || out != "aftergreen dev\n" {
		t.Fatalf("%d %q", code, out)
	}
}
