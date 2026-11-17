package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/foldnine/aftergreen/internal/httpcheck"
	"github.com/foldnine/aftergreen/internal/postgres"
)

// The JSON report is an integration surface: this golden output may only
// change by adding fields (and then ReportVersion stays 1) or with a new
// ReportVersion.
const goldenReport = `{
  "version": 1,
  "aftergreen_version": "v0.2.0-test",
  "deployment": {
    "name": "web",
    "revision": "abc"
  },
  "verdict": "FAILED",
  "summary": {
    "verified": 1,
    "failed": 1,
    "unknown": 1
  },
  "started_at": "2026-11-17T10:00:00.000Z",
  "completed_at": "2026-11-17T10:00:00.700Z",
  "duration_ms": 700,
  "checks": [
    {
      "id": "version",
      "type": "http",
      "verdict": "VERIFIED",
      "reasons": [],
      "attempts": 1,
      "duration_ms": 100,
      "observed_status": 200
    },
    {
      "id": "record",
      "type": "postgres",
      "verdict": "FAILED",
      "reasons": [
        "destination record missing"
      ],
      "attempts": 1,
      "duration_ms": 100
    },
    {
      "id": "health",
      "type": "http",
      "verdict": "UNKNOWN",
      "reasons": [
        "HTTP observation unavailable"
      ],
      "attempts": 1,
      "duration_ms": 100
    }
  ]
}
`

// stepClock advances 100ms on every call.
func stepClock() func() time.Time {
	t := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC).Add(-100 * time.Millisecond)
	return func() time.Time { t = t.Add(100 * time.Millisecond); return t }
}

func TestReportJSONGolden(t *testing.T) {
	eng := fakes(
		map[string]httpcheck.Verdict{
			"http://version": {Status: httpcheck.StatusVerified, ObservedStatus: 200},
			"http://health":  {Status: httpcheck.StatusUnknown, Reasons: []string{"HTTP observation unavailable"}},
		},
		map[string]postgres.Verdict{"record": {Status: postgres.StatusFailed, Reasons: []string{"destination record missing"}}},
	)
	rep, err := Run(context.Background(), file(httpCheck("version"), pgCheck("record"), httpCheck("health")), eng, Options{Now: stepClock()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := rep.JSON("v0.2.0-test")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != goldenReport {
		t.Fatalf("JSON report changed:\n%s", b)
	}
}

func TestReportJSONOmitsEmptyRevision(t *testing.T) {
	eng := fakes(map[string]httpcheck.Verdict{"http://a": {Status: httpcheck.StatusVerified, ObservedStatus: 200}}, nil)
	f := file(httpCheck("a"))
	f.Deployment.Revision = ""
	rep, _ := Run(context.Background(), f, eng, Options{Now: stepClock()})
	b, _ := rep.JSON("dev")
	if want := `"deployment": {
    "name": "web"
  },`; !strings.Contains(string(b), want) {
		t.Fatalf("%s", b)
	}
}
