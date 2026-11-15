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

func TestPostgresInvalidInput(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte(body), 0o600)
		return p
	}
	good := write("good.json", `{"version":1,"table":"leads","key_column":"event_id","key":"e1","expect":{"status":"ok"}}`)
	t.Setenv(postgresURLEnv, "")
	cases := map[string][]string{
		"no contract":       {"postgres"},
		"no env":            {"postgres", "--contract", good},
		"bad version":       {"postgres", "--contract", write("v2.json", `{"version":2,"table":"t","key_column":"k","key":"e","expect":{"a":"b"}}`)},
		"unknown field":     {"postgres", "--contract", write("typo.json", `{"version":1,"table":"t","key_column":"k","key":"e","expects":{"a":"b"}}`)},
		"sql in identifier": {"postgres", "--contract", write("inj.json", `{"version":1,"table":"t; DROP TABLE t","key_column":"k","key":"e","expect":{"a":"b"}}`)},
		"no expectations":   {"postgres", "--contract", write("empty.json", `{"version":1,"table":"t","key_column":"k","key":"e","expect":{}}`)},
		"no key":            {"postgres", "--contract", write("nokey.json", `{"version":1,"table":"t","key_column":"k","expect":{"a":"b"}}`)},
		"timeout":           {"postgres", "--contract", good, "--timeout", "90s"},
	}
	for name, args := range cases {
		if code, _, _ := runCLI(args...); code != exitInvalid {
			t.Errorf("%s: got %d want 2", name, code)
		}
	}
	t.Setenv(postgresURLEnv, "postgres://u:hunter2@[::1")
	code, _, errOut := runCLI("postgres", "--contract", good)
	if code != exitInvalid || strings.Contains(errOut, "hunter2") {
		t.Fatalf("bad DSN: %d %q", code, errOut)
	}
}
