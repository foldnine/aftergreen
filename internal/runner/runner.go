// Package runner executes the checks of a validated contract with the
// existing verification engines and aggregates their verdicts.
//
// Checks run one at a time in file order, so output and timing are
// deterministic. Each check gets an independent verdict. The overall verdict
// is FAILED if any check failed, otherwise UNKNOWN if any check is unknown,
// otherwise VERIFIED.
//
// A check with a retry policy is observed repeatedly, every Interval, until
// it has been VERIFIED Consecutive times in a row or its Deadline passes.
// During a rollout a wrong revision is expected for a while, so a FAILED
// observation inside the window is not final. When the deadline passes, the
// last completed observation decides:
//
//   - FAILED stays FAILED: the outcome was observed and was still wrong.
//   - UNKNOWN stays UNKNOWN: the outcome could not be observed.
//   - VERIFIED, but fewer than Consecutive times in a row: UNKNOWN, because
//     the outcome was not observed to be stable.
//
// An observation cut short by the deadline is not "completed" and does not
// replace an earlier one. If the run is cancelled before a retried check
// settles, the check is UNKNOWN: a mismatch only becomes final at the deadline.
package runner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/foldnine/aftergreen/internal/config"
	"github.com/foldnine/aftergreen/internal/httpcheck"
	"github.com/foldnine/aftergreen/internal/postgres"
)

const (
	Verified = "VERIFIED"
	Failed   = "FAILED"
	Unknown  = "UNKNOWN"
)

// Engines performs single observations. The CLI wires these to
// httpcheck.Verify and postgres.Verify; tests inject fakes. A non-nil error
// means the contract was invalid, which config validation should have caught.
type Engines struct {
	HTTP     func(context.Context, httpcheck.Contract) (httpcheck.Verdict, error)
	Postgres func(context.Context, postgres.Contract, time.Duration) (postgres.Verdict, error)
}

// Result is the outcome of one check.
type Result struct {
	ID             string
	Type           string
	Verdict        string
	Reasons        []string
	ObservedStatus int // HTTP only; 0 when no response was observed
	Attempts       int // observations made, including one cut short by the deadline
	Duration       time.Duration
}

// Attempt describes one observation of a retried check, for progress output.
type Attempt struct {
	CheckID string
	Number  int
	Verdict string
	Reasons []string
	// Next is the pause before the next attempt; 0 when this was the last.
	Next time.Duration
}

// Options configures Run.
type Options struct {
	Now      func() time.Time // defaults to time.Now
	Progress func(Attempt)    // optional; called after each attempt of a retried check
}

// Report is the outcome of a whole run.
type Report struct {
	Deployment config.Deployment
	Verdict    string
	Checks     []Result
	Started    time.Time
	Completed  time.Time
}

// Counts returns the number of checks per verdict.
func (r Report) Counts() (verified, failed, unknown int) {
	for _, c := range r.Checks {
		switch c.Verdict {
		case Verified:
			verified++
		case Failed:
			failed++
		default:
			unknown++
		}
	}
	return
}

// Overall combines verdicts: any FAILED wins, then any UNKNOWN (including
// unrecognised values), otherwise VERIFIED. No verdicts at all is UNKNOWN:
// nothing was observed.
func Overall(verdicts ...string) string {
	if len(verdicts) == 0 {
		return Unknown
	}
	out := Verified
	for _, v := range verdicts {
		switch v {
		case Failed:
			return Failed
		case Verified:
		default:
			out = Unknown
		}
	}
	return out
}

// Run executes every check in order. If ctx is cancelled, the running check
// and every check after it are reported UNKNOWN; cancellation never produces
// VERIFIED.
func Run(ctx context.Context, f *config.File, eng Engines, opts Options) (Report, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rep := Report{Deployment: f.Deployment, Started: now()}
	verdicts := make([]string, 0, len(f.Checks))
	for _, c := range f.Checks {
		start := now()
		res := Result{ID: c.ID, Type: c.Type}
		if ctx.Err() != nil {
			res.Verdict, res.Reasons = Unknown, []string{"not run: verification was cancelled"}
		} else {
			var err error
			if c.Retry == nil {
				res.Attempts = 1
				err = observe(ctx, c, eng, &res)
			} else {
				err = observeWithRetry(ctx, c, eng, opts.Progress, &res)
			}
			if err != nil {
				return rep, fmt.Errorf("check %q: %w", c.ID, err)
			}
			if c.Retry == nil && errors.Is(ctx.Err(), context.Canceled) && res.Verdict == Unknown {
				res.Reasons = append(res.Reasons, "verification was cancelled")
			}
		}
		res.Duration = now().Sub(start)
		rep.Checks = append(rep.Checks, res)
		verdicts = append(verdicts, res.Verdict)
	}
	rep.Verdict = Overall(verdicts...)
	rep.Completed = now()
	return rep, nil
}

func observeWithRetry(ctx context.Context, c config.Check, eng Engines, progress func(Attempt), res *Result) error {
	r := c.Retry
	dctx, cancel := context.WithTimeout(ctx, r.Deadline)
	defer cancel()
	var last *Result // last completed observation
	streak := 0
	for n := 1; ; n++ {
		cur := Result{ID: c.ID, Type: c.Type}
		if err := observe(dctx, c, eng, &cur); err != nil {
			return err
		}
		res.Attempts = n
		if dctx.Err() != nil && cur.Verdict != Verified && last != nil {
			// Cut short by the deadline or cancellation: keep the last completed one.
			if progress != nil {
				progress(Attempt{CheckID: c.ID, Number: n, Verdict: cur.Verdict,
					Reasons: append(append([]string{}, cur.Reasons...), "cut short; not counted")})
			}
			break
		}
		last = &cur
		if cur.Verdict == Verified {
			streak++
		} else {
			streak = 0
		}
		// Stop when stable, out of time, or when the next attempt could only
		// start at or after the deadline.
		deadline, _ := dctx.Deadline()
		done := streak >= r.Consecutive || dctx.Err() != nil || time.Until(deadline) <= r.Interval
		a := Attempt{CheckID: c.ID, Number: n, Verdict: cur.Verdict, Reasons: cur.Reasons}
		if !done {
			a.Next = r.Interval
		}
		if progress != nil {
			progress(a)
		}
		if done {
			break
		}
		t := time.NewTimer(r.Interval)
		select {
		case <-t.C:
		case <-dctx.Done():
			t.Stop()
		}
		if dctx.Err() != nil {
			break
		}
	}

	res.Verdict, res.Reasons, res.ObservedStatus = last.Verdict, last.Reasons, last.ObservedStatus
	switch {
	case res.Verdict == Verified && streak >= r.Consecutive:
		// stable
	case errors.Is(ctx.Err(), context.Canceled):
		res.Verdict = Unknown
		res.Reasons = append(append([]string{}, last.Reasons...),
			fmt.Sprintf("cancelled before the check settled (last observation: %s)", last.Verdict))
	case res.Verdict == Verified:
		res.Verdict = Unknown
		res.Reasons = []string{fmt.Sprintf("verified %d of %d required consecutive observations before the %s deadline", streak, r.Consecutive, r.Deadline)}
	default:
		res.Reasons = append(append([]string{}, last.Reasons...),
			fmt.Sprintf("still %s after %d attempts within the %s deadline", res.Verdict, res.Attempts, r.Deadline))
	}
	return nil
}

func observe(ctx context.Context, c config.Check, eng Engines, res *Result) error {
	switch {
	case c.HTTP != nil:
		v, err := eng.HTTP(ctx, *c.HTTP)
		if err != nil {
			return err
		}
		res.Verdict, res.Reasons, res.ObservedStatus = normalize(v.Status), v.Reasons, v.ObservedStatus
	case c.Postgres != nil:
		v, err := eng.Postgres(ctx, *c.Postgres, c.Timeout)
		if err != nil {
			return err
		}
		res.Verdict, res.Reasons = normalize(v.Status), v.Reasons
	default:
		return errors.New("check has no engine")
	}
	if res.Verdict != Verified && len(res.Reasons) == 0 {
		res.Reasons = []string{"no reason reported"}
	}
	return nil
}

// normalize maps anything that is not a known verdict to UNKNOWN.
func normalize(v string) string {
	switch v {
	case Verified, Failed:
		return v
	}
	return Unknown
}
