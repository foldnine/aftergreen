package runner

import (
	"encoding/json"
	"time"
)

// ReportVersion is the version of the JSON report format. Fields may be
// added within a version; existing fields keep their names, types and meaning.
const ReportVersion = 1

type jsonReport struct {
	Version           int            `json:"version"`
	AftergreenVersion string         `json:"aftergreen_version"`
	Deployment        jsonDeployment `json:"deployment"`
	Verdict           string         `json:"verdict"`
	Summary           jsonSummary    `json:"summary"`
	StartedAt         string         `json:"started_at"`
	CompletedAt       string         `json:"completed_at"`
	DurationMS        int64          `json:"duration_ms"`
	Checks            []jsonCheck    `json:"checks"`
}

type jsonDeployment struct {
	Name     string `json:"name"`
	Revision string `json:"revision,omitempty"`
}

type jsonSummary struct {
	Verified int `json:"verified"`
	Failed   int `json:"failed"`
	Unknown  int `json:"unknown"`
}

type jsonCheck struct {
	ID             string   `json:"id"`
	Type           string   `json:"type"`
	Verdict        string   `json:"verdict"`
	Reasons        []string `json:"reasons"`
	Attempts       int      `json:"attempts"`
	DurationMS     int64    `json:"duration_ms"`
	ObservedStatus int      `json:"observed_status,omitempty"`
}

// JSON renders the report for machines. It contains only what the text
// report shows: no URLs, keys, expected values, response bodies, row values
// or connection details. Timestamps are UTC, RFC 3339 with milliseconds.
func (r Report) JSON(aftergreenVersion string) ([]byte, error) {
	v, f, u := r.Counts()
	out := jsonReport{
		Version:           ReportVersion,
		AftergreenVersion: aftergreenVersion,
		Deployment:        jsonDeployment{Name: r.Deployment.Name, Revision: r.Deployment.Revision},
		Verdict:           r.Verdict,
		Summary:           jsonSummary{Verified: v, Failed: f, Unknown: u},
		StartedAt:         timestamp(r.Started),
		CompletedAt:       timestamp(r.Completed),
		DurationMS:        r.Completed.Sub(r.Started).Milliseconds(),
		Checks:            make([]jsonCheck, 0, len(r.Checks)),
	}
	for _, c := range r.Checks {
		reasons := c.Reasons
		if reasons == nil {
			reasons = []string{}
		}
		out.Checks = append(out.Checks, jsonCheck{
			ID: c.ID, Type: c.Type, Verdict: c.Verdict, Reasons: reasons,
			Attempts: c.Attempts, DurationMS: c.Duration.Milliseconds(), ObservedStatus: c.ObservedStatus,
		})
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func timestamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z07:00") }
