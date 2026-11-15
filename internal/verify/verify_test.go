package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluate(t *testing.T) {
	in := Input{Events: []Event{
		{ID: "ok", ExecutionStatus: "SUCCESS", ObservationAvailable: true, Expected: map[string]string{"count": "2"}, Actual: map[string]string{"count": "2"}},
		{ID: "bad", ExecutionStatus: "SUCCESS", ObservationAvailable: true, Expected: map[string]string{"email": "a@b"}, Actual: map[string]string{"email": ""}},
		{ID: "unknown", ExecutionStatus: "SUCCESS", ObservationAvailable: false, Expected: map[string]string{"count": "2"}},
	}}
	got := Evaluate(in)
	for i, want := range []string{"VERIFIED", "FAILED", "UNKNOWN"} {
		if got[i].Status != want {
			t.Fatalf("%d: got %s, want %s", i, got[i].Status, want)
		}
	}
	if !strings.Contains(got[1].Reasons[0], "email") {
		t.Fatal(got[1].Reasons)
	}
}
func TestReadRejectsDuplicate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "events.json")
	data := `{"events":[{"id":"same","expected":{"x":"1"}},{"id":"same","expected":{"x":"2"}}]}`
	if err := os.WriteFile(p, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(p); err == nil {
		t.Fatal("expected duplicate error")
	}
}
func TestReadRejectsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "empty.json")
	os.WriteFile(p, []byte(`{"events":[]}`), 0600)
	if _, err := Read(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestReadRejectsUnknownFieldsAndTrailingData(t *testing.T) {
	for _, data := range []string{
		`{"events":[{"id":"a","observaton_available":true,"expected":{"x":"1"}}]}`,
		`{"events":[{"id":"a","expected":{"x":"1"}}]} {"events":[]}`,
	} {
		p := filepath.Join(t.TempDir(), "e.json")
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Read(p); err == nil {
			t.Fatalf("accepted %s", data)
		}
	}
}
