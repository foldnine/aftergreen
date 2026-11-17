// Package runner executes the checks of a validated contract with the
// existing verification engines and aggregates their verdicts.
//
// Checks run one at a time in file order, so output and timing are
// deterministic. Each check gets an independent verdict. The overall verdict
// is FAILED if any check failed, otherwise UNKNOWN if any check is unknown,
// otherwise VERIFIED.
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
	Duration       time.Duration
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
// VERIFIED. now is the clock (time.Now in production).
func Run(ctx context.Context, f *config.File, eng Engines, now func() time.Time) (Report, error) {
	rep := Report{Deployment: f.Deployment, Started: now()}
	verdicts := make([]string, 0, len(f.Checks))
	for _, c := range f.Checks {
		start := now()
		res := Result{ID: c.ID, Type: c.Type}
		if ctx.Err() != nil {
			res.Verdict, res.Reasons = Unknown, []string{"not run: verification was cancelled"}
		} else {
			if err := runOne(ctx, c, eng, &res); err != nil {
				return rep, fmt.Errorf("check %q: %w", c.ID, err)
			}
			if errors.Is(ctx.Err(), context.Canceled) && res.Verdict == Unknown {
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

func runOne(ctx context.Context, c config.Check, eng Engines, res *Result) error {
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
