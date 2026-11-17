package config

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestJSONAssertions(t *testing.T) {
	doc := `version: 1
deployment: {name: web}
checks:
  - id: v
    type: http
    url: https://example.com/version
    expect:
      json:
        - {path: $.revision, equals: "${DEPLOY_SHA}"}
        - {path: $.replicas, equals: 3}
        - {path: $.ratio, equals: 0.5}
        - {path: $.ready, equals: true}
        - {path: $.note, equals: null}
        - {path: $.code, equals: "3"}
        - {path: $.literal, equals: "$${NOT_EXPANDED}"}
`
	f, err := Parse([]byte(doc), t.TempDir(), testEnv)
	if err != nil {
		t.Fatal(err)
	}
	got := f.Checks[0].HTTP.JSON
	want := []any{"a82f19c", json.Number("3"), json.Number("0.5"), true, nil, "3", "${NOT_EXPANDED}"}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i].Equals != want[i] {
			t.Errorf("%d: %#v, want %#v", i, got[i].Equals, want[i])
		}
	}
}

func TestInvalidJSONAndRetry(t *testing.T) {
	head := "version: 1\ndeployment: {name: web}\nchecks:\n  - id: a\n    type: http\n    url: https://example.com/\n"
	cases := map[string]struct{ extra, want string }{
		"empty json list":    {"    expect: {json: []}\n", "at least one assertion"},
		"null json":          {"    expect: {json: ~}\n", "json has no value"},
		"null path":          {"    expect: {json: [{path: ~, equals: x}]}\n", "path has no value"},
		"no path":            {"    expect: {json: [{equals: x}]}\n", "expect.json[0].path is required"},
		"no equals":          {"    expect: {json: [{path: $.a}]}\n", "expect.json[0].equals is required"},
		"unknown json field": {"    expect: {json: [{path: $.a, equals: x, matches: y}]}\n", `unknown field "matches"`},
		"bad path":           {"    expect: {json: [{path: '$..a', equals: x}]}\n", "supported syntax"},
		"filter expression":  {"    expect: {json: [{path: '$.a[?(@.b)]', equals: x}]}\n", "supported syntax"},
		"equals list":        {"    expect: {json: [{path: $.a, equals: [1]}]}\n", "string, number, boolean or null"},
		"equals map":         {"    expect: {json: [{path: $.a, equals: {b: 1}}]}\n", "string, number, boolean or null"},
		"equals hex":         {"    expect: {json: [{path: $.a, equals: 0x10}]}\n", "not a valid JSON number"},
		"equals inf":         {"    expect: {json: [{path: $.a, equals: .inf}]}\n", "not a valid JSON number"},
		"equals True":        {"    expect: {json: [{path: $.a, equals: True}]}\n", "true or false"},
		"equals tilde":       {"    expect: {json: [{path: $.a, equals: ~}]}\n", "write null as null"},
		"equals unset var":   {"    expect: {json: [{path: $.a, equals: '${NOPE}'}]}\n", "expect.json[0].equals: environment variable NOPE is not set"},
		"retry no deadline":  {"    retry: {interval: 5s}\n", "retry.deadline is required"},
		"retry unknown":      {"    retry: {deadline: 1m, attempts: 3}\n", `unknown field "attempts"`},
		"interval too short": {"    retry: {interval: 100ms, deadline: 1m}\n", "retry.interval"},
		"interval too long":  {"    retry: {interval: 10m, deadline: 20m}\n", "retry.interval"},
		"deadline too long":  {"    retry: {deadline: 2h}\n", "retry.deadline must be"},
		"deadline < timeout": {"    timeout: 30s\n    retry: {deadline: 10s}\n", "retry.deadline must be"},
		"consecutive zero":   {"    retry: {deadline: 1m, consecutive: 0}\n", "retry.consecutive"},
		"consecutive big":    {"    retry: {deadline: 1m, consecutive: 11}\n", "retry.consecutive"},
		"impossible streak":  {"    retry: {interval: 30s, deadline: 1m, consecutive: 3}\n", "too short for 3 consecutive"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(head+tc.extra), t.TempDir(), testEnv)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestRetry(t *testing.T) {
	head := "version: 1\ndeployment: {name: web}\nchecks:\n"
	doc := head + "  - {id: a, type: http, url: 'https://example.com/', retry: {deadline: 2m}}\n" +
		"  - {id: b, type: http, url: 'https://example.com/', timeout: 5s, retry: {interval: 10s, deadline: 3m, consecutive: 3}}\n" +
		"  - {id: c, type: http, url: 'https://example.com/'}\n"
	f, err := Parse([]byte(doc), t.TempDir(), testEnv)
	if err != nil {
		t.Fatal(err)
	}
	if r := f.Checks[0].Retry; r == nil || *r != (Retry{Interval: DefaultRetryInterval, Deadline: 2 * time.Minute, Consecutive: 1}) {
		t.Errorf("defaults: %+v", r)
	}
	if r := f.Checks[1].Retry; r == nil || *r != (Retry{Interval: 10 * time.Second, Deadline: 3 * time.Minute, Consecutive: 3}) {
		t.Errorf("explicit: %+v", r)
	}
	if f.Checks[2].Retry != nil {
		t.Error("retry must be opt-in")
	}
}
