package httpcheck

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestParsePath(t *testing.T) {
	good := map[string]int{"$": 0, "$.revision": 1, "$.build.sha": 2, "$.items[0].v": 3, "$[12]": 1, "$.a_b-c": 1, "$._x": 1}
	for p, n := range good {
		segs, err := parsePath(p)
		if err != nil || len(segs) != n {
			t.Errorf("%s: %v %v", p, segs, err)
		}
	}
	for _, p := range []string{
		"", "revision", "$.", "$..a", "$.1a", "$.-a", "$[01]", "$[-1]", "$[*]", "$.a[?(@.x)]",
		"$['a']", `$["a"]`, "$.a b", "$.a.length()", "$.ä", "$[1234567]",
		"$" + strings.Repeat(".a", 33), "$." + strings.Repeat("a", 300),
	} {
		if _, err := parsePath(p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
}

func TestValidateAssertions(t *testing.T) {
	ok := []JSONAssertion{{"$.a", nil}, {"$.b", true}, {"$.c", "x"}, {"$.d", json.Number("-1.5e3")}}
	if err := validateAssertions(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]JSONAssertion{
		{{"$.a", json.Number("0x10")}}, {{"$.a", json.Number("NaN")}}, {{"$.a", json.Number("01")}},
		{{"$.a", 3}}, {{"$.a", []any{}}}, {{"a", "x"}}, make([]JSONAssertion, MaxJSONAssertions+1),
	} {
		if err := validateAssertions(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestEvaluateJSON(t *testing.T) {
	body := `{"revision":"a82f19c","replicas":3,"ratio":0.5,"ready":true,"note":null,
	          "build":{"sha":"a82f19c","tags":["v1",2]},"empty":"","one":"1"}`
	cases := []struct {
		name   string
		a      JSONAssertion
		reason string // "" means the assertion holds
	}{
		{"string", JSONAssertion{"$.revision", "a82f19c"}, ""},
		{"nested", JSONAssertion{"$.build.sha", "a82f19c"}, ""},
		{"array index", JSONAssertion{"$.build.tags[1]", json.Number("2")}, ""},
		{"number", JSONAssertion{"$.replicas", json.Number("3")}, ""},
		{"number other notation", JSONAssertion{"$.replicas", json.Number("3.0")}, ""},
		{"number exponent", JSONAssertion{"$.ratio", json.Number("5e-1")}, ""},
		{"bool", JSONAssertion{"$.ready", true}, ""},
		{"null", JSONAssertion{"$.note", nil}, ""},
		{"empty string", JSONAssertion{"$.empty", ""}, ""},
		{"wrong string", JSONAssertion{"$.revision", "0ld0ld0"}, "json $.revision: value differs"},
		{"wrong number", JSONAssertion{"$.replicas", json.Number("4")}, "json $.replicas: value differs"},
		{"wrong bool", JSONAssertion{"$.ready", false}, "json $.ready: value differs"},
		{"string vs number", JSONAssertion{"$.one", json.Number("1")}, "json $.one: expected number, observed string"},
		{"number vs string", JSONAssertion{"$.replicas", "3"}, "json $.replicas: expected string, observed number"},
		{"null vs missing", JSONAssertion{"$.absent", nil}, "json $.absent: not found"},
		{"object vs string", JSONAssertion{"$.build", "x"}, "json $.build: expected string, observed object"},
		{"missing", JSONAssertion{"$.version", "x"}, "json $.version: not found"},
		{"index out of range", JSONAssertion{"$.build.tags[5]", "x"}, "json $.build.tags[5]: not found"},
		{"index into object", JSONAssertion{"$.build[0]", "x"}, "json $.build[0]: not found"},
		{"member of string", JSONAssertion{"$.revision.x", "x"}, "json $.revision.x: not found"},
	}
	for _, tc := range cases {
		reasons, unk := evaluateJSON([]byte(body), []JSONAssertion{tc.a})
		got := strings.Join(reasons, "; ")
		if unk != "" || got != tc.reason {
			t.Errorf("%s: reasons %q unknown %q, want %q", tc.name, got, unk, tc.reason)
		}
	}
	// The root path addresses scalar documents.
	if r, _ := evaluateJSON([]byte(`"a82f19c"`), []JSONAssertion{{"$", "a82f19c"}}); len(r) != 0 {
		t.Errorf("root: %v", r)
	}
}

func TestEvaluateJSONUntrustedBodies(t *testing.T) {
	as := []JSONAssertion{{"$.revision", "a82f19c"}}
	malformed := []string{
		"", "<html>maintenance</html>", `{"revision":"a82f19c"`, `{"revision":"a82f19c"} {}`,
		`{"revision":"a82f19c"}x`, "{\"revision\":\"a82f19c\xff\"}", `{revision:"a82f19c"}`,
	}
	for _, b := range malformed {
		reasons, unk := evaluateJSON([]byte(b), as)
		if unk != "" || len(reasons) != 1 || reasons[0] != "response body is not valid JSON" {
			t.Errorf("%q: %v %q", b, reasons, unk)
		}
	}
	if _, unk := evaluateJSON([]byte(strings.Repeat("[", 100)+strings.Repeat("]", 100)), as); unk == "" {
		t.Error("over-deep documents must be UNKNOWN")
	}
	if _, unk := evaluateJSON([]byte(`{"n":1e99999999999}`), []JSONAssertion{{"$.n", json.Number("1")}}); unk == "" {
		t.Error("numbers beyond exact range must be UNKNOWN")
	}
	if err := validateAssertions([]JSONAssertion{{"$.n", json.Number("1e99999999999")}}); err == nil {
		t.Error("unparseable expected number accepted")
	}
	// Duplicate keys: which one is "the" revision depends on the parser.
	if _, unk := evaluateJSON([]byte(`{"revision":"0ld0ld0","revision":"a82f19c"}`), as); unk == "" {
		t.Error("duplicate keys must be UNKNOWN")
	}
	if _, unk := evaluateJSON([]byte(`{"a":{"x":1,"x":2},"revision":"a82f19c"}`), as); unk == "" {
		t.Error("nested duplicate keys must be UNKNOWN")
	}
}

func TestVerifyJSON(t *testing.T) {
	var body string
	status := 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer srv.Close()
	c := Contract{URL: srv.URL, ExpectedStatus: 200, Timeout: 5 * time.Second,
		JSON: []JSONAssertion{{"$.revision", "a82f19c-secret"}}}
	run := func() Verdict {
		v, err := Verify(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	body = `{"revision":"a82f19c-secret"}`
	if v := run(); v.Status != StatusVerified {
		t.Fatalf("verified: %+v", v)
	}
	body = `{"revision":"0ld0ld0-secret"}`
	v := run()
	if v.Status != StatusFailed || strings.Contains(strings.Join(v.Reasons, ""), "secret") {
		t.Fatalf("mismatch: %+v", v)
	}
	body = `<html>maintenance</html>`
	if v := run(); v.Status != StatusFailed {
		t.Fatalf("malformed: %+v", v)
	}
	body = `{"revision":"x","revision":"a82f19c-secret"}`
	if v := run(); v.Status != StatusUnknown {
		t.Fatalf("duplicate: %+v", v)
	}
	// Wrong status: FAILED on the status alone; the error page is not parsed.
	status, body = 503, `{"revision":"x","revision":"y"}`
	if v := run(); v.Status != StatusFailed || len(v.Reasons) != 1 {
		t.Fatalf("status: %+v", v)
	}
	// Bodies over the limit cannot be evaluated: UNKNOWN, as for --contains.
	status, body = 200, `{"revision":"a82f19c-secret","pad":"`+strings.Repeat("x", MaxBodyBytes)+`"}`
	if v := run(); v.Status != StatusUnknown {
		t.Fatalf("oversized: %+v", v)
	}
	// contains and JSON together: both must hold.
	c.ExpectedBodySubstring = "nope"
	body = `{"revision":"a82f19c-secret"}`
	if v := run(); v.Status != StatusFailed || len(v.Reasons) != 1 {
		t.Fatalf("contains+json: %+v", v)
	}
}

func TestVerifyRejectsInvalidAssertions(t *testing.T) {
	c := Contract{URL: "http://127.0.0.1:1/", ExpectedStatus: 200, Timeout: time.Second,
		JSON: []JSONAssertion{{"$.a[*]", "x"}}}
	if _, err := Verify(context.Background(), c); err == nil {
		t.Fatal("invalid path accepted")
	}
}
