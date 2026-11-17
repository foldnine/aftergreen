package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/foldnine/aftergreen/internal/config"
	"github.com/foldnine/aftergreen/internal/runner"
)

// versionServer serves /version with the given revision and /health with 200.
func versionServer(t *testing.T, revision string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			w.Write([]byte(`{"revision":"` + revision + `"}`))
		case "/health":
			w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "aftergreen.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func twoChecks(base string) string {
	return `version: 1
deployment:
  name: payment-api
  revision: "${DEPLOY_SHA}"
checks:
  - id: health
    type: http
    url: ` + base + `/health
  - id: correct-version
    type: http
    url: ` + base + `/version?token=${API_TOKEN}
    expect:
      status: 200
      contains: '"revision":"${DEPLOY_SHA}"'
`
}

func TestVerifyVerified(t *testing.T) {
	srv := versionServer(t, "a82f19c")
	t.Setenv("DEPLOY_SHA", "a82f19c")
	t.Setenv("API_TOKEN", "tok-not-printed")
	code, out, errOut := runCLI("verify", "-f", writeConfig(t, twoChecks(srv.URL)))
	if code != exitVerified {
		t.Fatalf("exit %d\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		"Deployment: payment-api\n", "Revision:   a82f19c\n",
		"VERIFIED  health\n", "VERIFIED  correct-version\n",
		"Deployment verification VERIFIED\n", "2 verified\n0 failed\n0 unknown\n", "Duration: ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if errOut != "" || strings.Contains(out, "tok-not-printed") || strings.Contains(out, srv.URL) {
		t.Errorf("leak or noise: stdout=%s stderr=%s", out, errOut)
	}
}

func TestVerifyWrongRevisionFails(t *testing.T) {
	srv := versionServer(t, "0ld0ld0")
	t.Setenv("DEPLOY_SHA", "a82f19c")
	t.Setenv("API_TOKEN", "tok-not-printed")
	code, out, _ := runCLI("verify", "--file", writeConfig(t, twoChecks(srv.URL)))
	if code != exitFailed || !strings.Contains(out, "FAILED    correct-version\n") ||
		!strings.Contains(out, "- response body did not contain the expected text") ||
		!strings.Contains(out, "Deployment verification FAILED") || !strings.Contains(out, "1 verified\n1 failed\n0 unknown") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	if strings.Contains(out, "0ld0ld0") || strings.Contains(out, "tok-not-printed") {
		t.Errorf("response body or token printed:\n%s", out)
	}
}

func TestVerifyWrongStatusFails(t *testing.T) {
	srv := versionServer(t, "a82f19c")
	cfg := "version: 1\ndeployment: {name: web}\nchecks:\n  - {id: gone, type: http, url: '" + srv.URL + "/missing'}\n"
	if code, out, _ := runCLI("verify", "-f", writeConfig(t, cfg)); code != exitFailed || !strings.Contains(out, "expected HTTP status 200, observed 404") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestVerifyUnreachableIsUnknown(t *testing.T) {
	srv := versionServer(t, "a82f19c")
	cfg := "version: 1\ndeployment: {name: web}\nchecks:\n" +
		"  - {id: up, type: http, url: '" + srv.URL + "/health'}\n" +
		"  - {id: down, type: http, url: '" + unreachableURL(t) + "', timeout: 1s}\n"
	code, out, _ := runCLI("verify", "-f", writeConfig(t, cfg))
	if code != exitUnknown || !strings.Contains(out, "UNKNOWN   down\n") || !strings.Contains(out, "Deployment verification UNKNOWN") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestVerifyFailedBeatsUnknown(t *testing.T) {
	srv := versionServer(t, "a82f19c")
	cfg := "version: 1\ndeployment: {name: web}\nchecks:\n" +
		"  - {id: down, type: http, url: '" + unreachableURL(t) + "', timeout: 1s}\n" +
		"  - {id: gone, type: http, url: '" + srv.URL + "/missing'}\n"
	if code, out, _ := runCLI("verify", "-f", writeConfig(t, cfg)); code != exitFailed || !strings.Contains(out, "Deployment verification FAILED") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestVerifyInvalidInput(t *testing.T) {
	t.Setenv("DEPLOY_SHA", "")
	os.Unsetenv("DEPLOY_SHA")
	dir := t.TempDir()
	cases := map[string]struct {
		args []string
		want string
	}{
		"missing file":     {[]string{"verify", "-f", filepath.Join(dir, "nope.yaml")}, "not found"},
		"default missing":  {[]string{"verify"}, "aftergreen.yaml not found"},
		"stray argument":   {[]string{"verify", "aftergreen.yaml"}, "unexpected argument"},
		"unknown flag":     {[]string{"verify", "--json-out"}, "flag provided but not defined"},
		"unset variable":   {[]string{"verify", "-f", writeConfig(t, twoChecks("http://127.0.0.1:1"))}, "environment variable DEPLOY_SHA is not set"},
		"bad version":      {[]string{"verify", "-f", writeConfig(t, "version: 9\n")}, "unsupported version 9"},
		"unknown type":     {[]string{"verify", "-f", writeConfig(t, "version: 1\ndeployment: {name: w}\nchecks:\n  - {id: a, type: grpc}\n")}, `unknown type "grpc"`},
		"duplicate id":     {[]string{"verify", "-f", writeConfig(t, "version: 1\ndeployment: {name: w}\nchecks:\n  - {id: a, type: http, url: 'http://h/'}\n  - {id: a, type: http, url: 'http://h/'}\n")}, "duplicate id"},
		"postgres no conn": {[]string{"verify", "-f", pgConfig(t, "k")}, "set AFTERGREEN_POSTGRES_URL"},
	}
	t.Chdir(dir)
	t.Setenv(postgresURLEnv, "")
	for name, tc := range cases {
		code, out, errOut := runCLI(tc.args...)
		if code != exitInvalid || out != "" || !strings.Contains(errOut, tc.want) {
			t.Errorf("%s: exit %d, stdout %q, stderr %q (want %q)", name, code, out, errOut, tc.want)
		}
	}
}

func pgConfig(t *testing.T, key string) string {
	t.Helper()
	p := writeConfig(t, "version: 1\ndeployment: {name: web}\nchecks:\n  - {id: db, type: postgres, contract: deploy.json, key: '"+key+"'}\n")
	os.WriteFile(filepath.Join(filepath.Dir(p), "deploy.json"), []byte(`{"version":1,"table":"deploys","key_column":"deploy_id","expect":{"state":"live"}}`), 0o600)
	return p
}

func TestVerifyPostgresConnectionSecrets(t *testing.T) {
	t.Setenv(postgresURLEnv, "postgres://u:hunter2@[::1")
	code, out, errOut := runCLI("verify", "-f", pgConfig(t, "k"))
	if code != exitInvalid || strings.Contains(out+errOut, "hunter2") {
		t.Fatalf("bad DSN: %d %q %q", code, out, errOut)
	}
	// Unreachable database: UNKNOWN, never VERIFIED, password never printed.
	t.Setenv(postgresURLEnv, "postgres://nobody:pw-not-printed@127.0.0.1:1/x?connect_timeout=2")
	code, out, errOut = runCLI("verify", "-f", pgConfig(t, "run-1001"))
	if code != exitUnknown || !strings.Contains(out, "UNKNOWN   db\n") || strings.Contains(out+errOut, "pw-not-printed") || strings.Contains(out+errOut, "run-1001") {
		t.Fatalf("unreachable: %d %q %q", code, out, errOut)
	}
}

// The exit code and the printed overall verdict must always agree.
func TestExitCodeMatchesOverall(t *testing.T) {
	want := map[string]int{runner.Verified: exitVerified, runner.Failed: exitFailed, runner.Unknown: exitUnknown}
	sets := [][]string{
		{"VERIFIED"}, {"FAILED"}, {"UNKNOWN"}, {"VERIFIED", "UNKNOWN"}, {"UNKNOWN", "FAILED"},
		{"VERIFIED", "FAILED", "UNKNOWN"}, {"VERIFIED", "VERIFIED"}, {"bogus"},
	}
	for _, s := range sets {
		if got := exitCode(s...); got != want[runner.Overall(s...)] {
			t.Errorf("%v: exitCode %d, Overall %s", s, got, runner.Overall(s...))
		}
	}
}

// Shipped examples must stay valid.
func TestExamplesAreValid(t *testing.T) {
	paths, _ := filepath.Glob("../../examples/verification/*.yaml")
	if len(paths) < 2 {
		t.Fatalf("examples not found: %v", paths)
	}
	lookup := func(k string) (string, bool) { return "example-" + k, true }
	for _, p := range paths {
		if _, err := config.Load(p, lookup); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func unreachableURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	return u + "/"
}
