package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// rolloutServer serves the old revision for the first `before` requests, then
// the new one, like a deployment that converges after a while.
func rolloutServer(t *testing.T, before int32) *httptest.Server {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rev := "0ld0ld0"
		if n.Add(1) > before {
			rev = "a82f19c"
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"revision":"` + rev + `","replicas":3}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func retryConfig(base, retry string) string {
	return `version: 1
deployment: {name: payment-api, revision: "${DEPLOY_SHA}"}
checks:
  - id: correct-release
    type: http
    url: ` + base + `/version
    timeout: 1s
    expect:
      status: 200
      json:
        - {path: $.revision, equals: "${DEPLOY_SHA}"}
        - {path: $.replicas, equals: 3}
    retry: ` + retry + "\n"
}

func TestVerifyRetryConverges(t *testing.T) {
	srv := rolloutServer(t, 1)
	t.Setenv("DEPLOY_SHA", "a82f19c")
	code, out, errOut := runCLI("verify", "-f", writeConfig(t, retryConfig(srv.URL, "{interval: 1s, deadline: 10s, consecutive: 2}")))
	if code != exitVerified || !strings.Contains(out, "VERIFIED  correct-release (3 attempts)") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	// Progress is on stderr, never in the report on stdout.
	if !strings.Contains(errOut, "aftergreen: correct-release: attempt 1 FAILED (json $.revision: value differs), retrying in 1s") ||
		!strings.Contains(errOut, "attempt 3 VERIFIED\n") || strings.Contains(out, "retrying") {
		t.Fatalf("progress:\n%s", errOut)
	}
	if strings.Contains(out+errOut, "0ld0ld0") {
		t.Fatal("response value printed")
	}
}

func TestVerifyRetryExhausted(t *testing.T) {
	srv := rolloutServer(t, 1000)
	t.Setenv("DEPLOY_SHA", "a82f19c")
	code, out, _ := runCLI("verify", "-f", writeConfig(t, retryConfig(srv.URL, "{interval: 1s, deadline: 2s}")))
	if code != exitFailed || !strings.Contains(out, "FAILED    correct-release (2 attempts)") ||
		!strings.Contains(out, "- json $.revision: value differs") || !strings.Contains(out, "still FAILED after 2 attempts within the 2s deadline") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}

func TestVerifyJSONOneShot(t *testing.T) {
	srv := rolloutServer(t, 0)
	t.Setenv("DEPLOY_SHA", "a82f19c")
	cfg := strings.Replace(retryConfig(srv.URL, "x"), "    retry: x\n", "", 1)
	if code, out, errOut := runCLI("verify", "-f", writeConfig(t, cfg)); code != exitVerified || errOut != "" {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	t.Setenv("DEPLOY_SHA", "b7e01d4")
	if code, out, _ := runCLI("verify", "-f", writeConfig(t, cfg)); code != exitFailed || !strings.Contains(out, "json $.revision: value differs") {
		t.Fatalf("exit %d\n%s", code, out)
	}
}
