package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/foldnine/aftergreen/internal/config"
	"github.com/foldnine/aftergreen/internal/httpcheck"
	"github.com/foldnine/aftergreen/internal/postgres"
)

func TestOverall(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{Verified}, Verified},
		{[]string{Verified, Verified}, Verified},
		{[]string{Verified, Unknown}, Unknown},
		{[]string{Unknown, Failed}, Failed},
		{[]string{Failed, Unknown, Verified}, Failed},
		{[]string{"weird"}, Unknown},
		{[]string{""}, Unknown},
		{nil, Unknown},
	}
	for _, c := range cases {
		if got := Overall(c.in...); got != c.want {
			t.Errorf("Overall(%v) = %s, want %s", c.in, got, c.want)
		}
	}
}

func file(checks ...config.Check) *config.File {
	return &config.File{Version: 1, Deployment: config.Deployment{Name: "web", Revision: "abc"}, Checks: checks}
}

func httpCheck(id string) config.Check {
	return config.Check{ID: id, Type: config.TypeHTTP, Timeout: time.Second, HTTP: &httpcheck.Contract{URL: "http://" + id}}
}

func pgCheck(id string) config.Check {
	return config.Check{ID: id, Type: config.TypePostgres, Timeout: time.Second, Postgres: &postgres.Contract{Table: id, KeyValue: "k"}}
}

// fakes returns engines that answer from a per-check verdict table.
func fakes(httpVerdicts map[string]httpcheck.Verdict, pgVerdicts map[string]postgres.Verdict) Engines {
	return Engines{
		HTTP: func(_ context.Context, c httpcheck.Contract) (httpcheck.Verdict, error) {
			return httpVerdicts[c.URL], nil
		},
		Postgres: func(_ context.Context, c postgres.Contract, _ time.Duration) (postgres.Verdict, error) {
			return pgVerdicts[c.Table], nil
		},
	}
}

func TestRunAggregatesInOrder(t *testing.T) {
	eng := fakes(
		map[string]httpcheck.Verdict{
			"http://version": {Status: httpcheck.StatusVerified, ObservedStatus: 200},
			"http://health":  {Status: httpcheck.StatusUnknown, Reasons: []string{"HTTP observation timed out"}},
		},
		map[string]postgres.Verdict{
			"record": {Status: postgres.StatusFailed, Reasons: []string{"destination record missing"}},
		},
	)
	rep, err := Run(context.Background(), file(httpCheck("version"), pgCheck("record"), httpCheck("health")), eng, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Verdict != Failed || rep.Deployment.Revision != "abc" || len(rep.Checks) != 3 {
		t.Fatalf("%+v", rep)
	}
	want := []struct{ id, typ, verdict string }{
		{"version", "http", Verified}, {"record", "postgres", Failed}, {"health", "http", Unknown},
	}
	for i, w := range want {
		c := rep.Checks[i]
		if c.ID != w.id || c.Type != w.typ || c.Verdict != w.verdict {
			t.Errorf("check %d: %+v, want %+v", i, c, w)
		}
	}
	if rep.Checks[0].ObservedStatus != 200 || rep.Checks[1].Reasons[0] != "destination record missing" {
		t.Errorf("details lost: %+v", rep.Checks)
	}
	if v, f, u := rep.Counts(); v != 1 || f != 1 || u != 1 {
		t.Errorf("counts %d %d %d", v, f, u)
	}
	if rep.Completed.Before(rep.Started) {
		t.Error("completed before started")
	}
}

func TestUnknownPropagates(t *testing.T) {
	eng := fakes(
		map[string]httpcheck.Verdict{"http://a": {Status: httpcheck.StatusVerified}},
		map[string]postgres.Verdict{"b": {Status: postgres.StatusUnknown, Reasons: []string{"cannot start read-only transaction (SQLSTATE 28P01: authentication failed)"}}},
	)
	rep, err := Run(context.Background(), file(httpCheck("a"), pgCheck("b")), eng, time.Now)
	if err != nil || rep.Verdict != Unknown {
		t.Fatalf("%v %+v", err, rep)
	}
}

func TestUnrecognisedVerdictIsUnknown(t *testing.T) {
	eng := fakes(map[string]httpcheck.Verdict{"http://a": {Status: "PASSED"}}, nil)
	rep, _ := Run(context.Background(), file(httpCheck("a")), eng, time.Now)
	if rep.Verdict != Unknown || rep.Checks[0].Verdict != Unknown || len(rep.Checks[0].Reasons) == 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestEngineErrorStopsRun(t *testing.T) {
	eng := Engines{HTTP: func(context.Context, httpcheck.Contract) (httpcheck.Verdict, error) {
		return httpcheck.Verdict{}, errors.New("URL is required")
	}}
	if _, err := Run(context.Background(), file(httpCheck("a")), eng, time.Now); err == nil {
		t.Fatal("expected error")
	}
}

func TestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	eng := Engines{HTTP: func(ctx context.Context, c httpcheck.Contract) (httpcheck.Verdict, error) {
		calls++
		if c.URL == "http://a" {
			return httpcheck.Verdict{Status: httpcheck.StatusVerified}, nil
		}
		cancel() // interrupted while the second check runs
		<-ctx.Done()
		return httpcheck.Verdict{Status: httpcheck.StatusUnknown, Reasons: []string{"HTTP observation timed out"}}, nil
	}}
	rep, err := Run(ctx, file(httpCheck("a"), httpCheck("b"), httpCheck("c")), eng, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("check after cancellation was executed (%d calls)", calls)
	}
	got := []string{rep.Checks[0].Verdict, rep.Checks[1].Verdict, rep.Checks[2].Verdict}
	if got[0] != Verified || got[1] != Unknown || got[2] != Unknown || rep.Verdict != Unknown {
		t.Fatalf("verdicts %v overall %s", got, rep.Verdict)
	}
	if r := rep.Checks[2].Reasons; len(r) != 1 || r[0] != "not run: verification was cancelled" {
		t.Errorf("reasons %v", r)
	}
}
