package runner

import (
	"context"
	"errors"
	"strings"
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
	rep, err := Run(context.Background(), file(httpCheck("version"), pgCheck("record"), httpCheck("health")), eng, Options{})
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
	rep, err := Run(context.Background(), file(httpCheck("a"), pgCheck("b")), eng, Options{})
	if err != nil || rep.Verdict != Unknown {
		t.Fatalf("%v %+v", err, rep)
	}
}

func TestUnrecognisedVerdictIsUnknown(t *testing.T) {
	eng := fakes(map[string]httpcheck.Verdict{"http://a": {Status: "PASSED"}}, nil)
	rep, _ := Run(context.Background(), file(httpCheck("a")), eng, Options{})
	if rep.Verdict != Unknown || rep.Checks[0].Verdict != Unknown || len(rep.Checks[0].Reasons) == 0 {
		t.Fatalf("%+v", rep)
	}
}

func TestEngineErrorStopsRun(t *testing.T) {
	eng := Engines{HTTP: func(context.Context, httpcheck.Contract) (httpcheck.Verdict, error) {
		return httpcheck.Verdict{}, errors.New("URL is required")
	}}
	if _, err := Run(context.Background(), file(httpCheck("a")), eng, Options{}); err == nil {
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
	rep, err := Run(ctx, file(httpCheck("a"), httpCheck("b"), httpCheck("c")), eng, Options{})
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

// scripted returns an HTTP engine that answers with the given verdicts in
// order (the last one repeats). "SLOW" blocks until the context ends.
func scripted(seq ...string) (Engines, *int) {
	calls := new(int)
	return Engines{HTTP: func(ctx context.Context, _ httpcheck.Contract) (httpcheck.Verdict, error) {
		i := *calls
		*calls++
		if i >= len(seq) {
			i = len(seq) - 1
		}
		switch seq[i] {
		case "SLOW":
			<-ctx.Done()
			return httpcheck.Verdict{Status: httpcheck.StatusUnknown, Reasons: []string{"HTTP observation timed out"}}, nil
		case Verified:
			return httpcheck.Verdict{Status: Verified, ObservedStatus: 200}, nil
		case Failed:
			return httpcheck.Verdict{Status: Failed, ObservedStatus: 200, Reasons: []string{"response body did not contain the expected text"}}, nil
		}
		return httpcheck.Verdict{Status: Unknown, Reasons: []string{"HTTP observation unavailable"}}, nil
	}}, calls
}

func retried(interval, deadline time.Duration, consecutive int) config.Check {
	c := httpCheck("version")
	c.Retry = &config.Retry{Interval: interval, Deadline: deadline, Consecutive: consecutive}
	return c
}

func TestRetry(t *testing.T) {
	const ms = time.Millisecond
	cases := []struct {
		name        string
		seq         []string
		check       config.Check
		verdict     string
		attempts    int
		reasonMatch string
	}{
		// 00:00 wrong revision, 00:05 wrong revision, 00:10 expected, 00:15 expected.
		{"rollout converges", []string{Failed, Failed, Verified, Verified}, retried(5*ms, 2*time.Second, 2), Verified, 4, ""},
		{"verified first try", []string{Verified}, retried(5*ms, time.Second, 1), Verified, 1, ""},
		{"unknown then verified", []string{Unknown, Verified}, retried(5*ms, time.Second, 1), Verified, 2, ""},
		{"exhausted on wrong revision", []string{Failed}, retried(10*ms, 100*ms, 1), Failed, -1, "still FAILED after"},
		{"exhausted unobservable", []string{Unknown}, retried(10*ms, 100*ms, 1), Unknown, -1, "still UNKNOWN after"},
		{"flapping never stable", []string{Verified, Failed}, retried(10*ms, 100*ms, 2), Failed, -1, "still FAILED"},
		{"stable streak resets", []string{Verified, Failed, Verified, Verified}, retried(5*ms, time.Second, 2), Verified, 4, ""},
		{"not enough consecutive", []string{Failed, Failed, Failed, Failed, Failed, Failed, Failed, Failed, Verified}, retried(10*ms, 100*ms, 3), Unknown, -1, "consecutive observations before the"},
		// The last completed observation decides; one cut short by the deadline does not count.
		{"cut short keeps last completed", []string{Failed, "SLOW"}, retried(5*ms, 100*ms, 1), Failed, 2, "still FAILED after 2 attempts"},
		{"first attempt cut short", []string{"SLOW"}, retried(5*ms, 50*ms, 1), Unknown, 1, "timed out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, calls := scripted(tc.seq...)
			var progress []Attempt
			start := time.Now()
			rep, err := Run(context.Background(), file(tc.check), eng, Options{Progress: func(a Attempt) { progress = append(progress, a) }})
			if err != nil {
				t.Fatal(err)
			}
			res := rep.Checks[0]
			if res.Verdict != tc.verdict || rep.Verdict != tc.verdict {
				t.Fatalf("verdict %s (overall %s), want %s; reasons %v", res.Verdict, rep.Verdict, tc.verdict, res.Reasons)
			}
			if tc.attempts > 0 && (res.Attempts != tc.attempts || *calls != tc.attempts) {
				t.Errorf("attempts %d calls %d, want %d", res.Attempts, *calls, tc.attempts)
			}
			if tc.attempts < 0 && res.Attempts < 2 {
				t.Errorf("expected several attempts, got %d", res.Attempts)
			}
			joined := strings.Join(res.Reasons, "; ")
			if tc.verdict != Verified && !strings.Contains(joined, tc.reasonMatch) {
				t.Errorf("reasons %q, want %q", joined, tc.reasonMatch)
			}
			if tc.verdict == Verified && len(res.Reasons) != 0 {
				t.Errorf("verified with reasons %v", res.Reasons)
			}
			// Bounded: never meaningfully past the deadline.
			if el := time.Since(start); el > tc.check.Retry.Deadline+500*ms {
				t.Errorf("took %s, deadline %s", el, tc.check.Retry.Deadline)
			}
			if len(progress) == 0 || progress[len(progress)-1].Next != 0 {
				t.Errorf("progress: %+v", progress)
			}
		})
	}
}

func TestRetryCancelledIsUnknown(t *testing.T) {
	eng, _ := scripted(Failed)
	ctx, cancel := context.WithCancel(context.Background())
	opts := Options{Progress: func(a Attempt) {
		if a.Number == 2 {
			cancel()
		}
	}}
	start := time.Now()
	rep, err := Run(ctx, file(retried(10*time.Millisecond, time.Minute, 1), httpCheck("after")), eng, opts)
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("cancellation did not stop the retry loop")
	}
	// A mismatch is only final at the deadline; cancelled earlier, it is UNKNOWN.
	if rep.Checks[0].Verdict != Unknown || !strings.Contains(strings.Join(rep.Checks[0].Reasons, ";"), "cancelled before the check settled (last observation: FAILED)") {
		t.Fatalf("%+v", rep.Checks[0])
	}
	if rep.Checks[1].Verdict != Unknown || rep.Verdict != Unknown {
		t.Fatalf("%+v", rep)
	}
}

func TestRetryAppliesToPostgres(t *testing.T) {
	calls := 0
	eng := Engines{Postgres: func(context.Context, postgres.Contract, time.Duration) (postgres.Verdict, error) {
		calls++
		if calls < 3 {
			return postgres.Verdict{Status: postgres.StatusFailed, Reasons: []string{"destination record missing"}}, nil
		}
		return postgres.Verdict{Status: postgres.StatusVerified}, nil
	}}
	c := pgCheck("deploys")
	c.Retry = &config.Retry{Interval: 5 * time.Millisecond, Deadline: time.Second, Consecutive: 1}
	rep, _ := Run(context.Background(), file(c), eng, Options{})
	if rep.Verdict != Verified || rep.Checks[0].Attempts != 3 {
		t.Fatalf("%+v", rep.Checks[0])
	}
}
