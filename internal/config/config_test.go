package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func env(vars map[string]string) LookupFunc {
	return func(k string) (string, bool) { v, ok := vars[k]; return v, ok }
}

var testEnv = env(map[string]string{"DEPLOY_SHA": "a82f19c", "DEPLOY_ID": "run-1001", "EMPTY": "", "TOKEN": "s3cr3t-token"})

func dirWithContract(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "checks"), 0o755)
	os.WriteFile(filepath.Join(dir, "checks", "deploy.json"),
		[]byte(`{"version":1,"table":"deploys","key_column":"deploy_id","key":"stale-key","expect":{"state":"live"}}`), 0o600)
	return dir
}

const valid = `
version: 1
deployment:
  name: payment-api
  revision: "${DEPLOY_SHA}"
checks:
  - id: correct-version
    type: http
    url: https://api.example.com/version?token=${TOKEN}
    timeout: 5s
    expect:
      status: 200
      contains: "${DEPLOY_SHA}"
  - id: database-record
    type: postgres
    contract: ./checks/deploy.json
    key: "${DEPLOY_ID}"
`

func TestValid(t *testing.T) {
	f, err := Parse([]byte(valid), dirWithContract(t), testEnv)
	if err != nil {
		t.Fatal(err)
	}
	if f.Deployment.Name != "payment-api" || f.Deployment.Revision != "a82f19c" || len(f.Checks) != 2 || !f.UsesPostgres {
		t.Fatalf("%+v", f)
	}
	h := f.Checks[0]
	if h.Type != TypeHTTP || h.HTTP.URL != "https://api.example.com/version?token=s3cr3t-token" ||
		h.HTTP.ExpectedBodySubstring != "a82f19c" || h.HTTP.ExpectedStatus != 200 || h.HTTP.Timeout != 5*time.Second {
		t.Fatalf("http: %+v", h.HTTP)
	}
	p := f.Checks[1]
	// The key comes from the configuration, never from the contract file.
	if p.Type != TypePostgres || p.Postgres.KeyValue != "run-1001" || p.Postgres.Table != "deploys" || p.Timeout != DefaultTimeout {
		t.Fatalf("postgres: %+v", p.Postgres)
	}
}

func TestDefaults(t *testing.T) {
	f, err := Parse([]byte("version: 1\ndeployment: {name: web}\nchecks:\n  - {id: up, type: http, url: 'http://127.0.0.1:8080/'}\n"), t.TempDir(), testEnv)
	if err != nil {
		t.Fatal(err)
	}
	c := f.Checks[0]
	if c.HTTP.ExpectedStatus != 200 || c.HTTP.Timeout != DefaultTimeout || f.Deployment.Revision != "" || f.UsesPostgres {
		t.Fatalf("%+v %+v", c.HTTP, f)
	}
}

func TestInvalid(t *testing.T) {
	head := "version: 1\ndeployment: {name: web}\nchecks:\n"
	http := func(extra string) string {
		return head + "  - id: a\n    type: http\n    url: https://example.com/\n" + extra
	}
	pg := func(extra string) string { return head + "  - id: db\n    type: postgres\n" + extra }
	cases := map[string]struct{ doc, want string }{
		"empty":                {"", "empty"},
		"not a mapping":        {"- 1\n", "invalid YAML"},
		"no version":           {"deployment: {name: web}\nchecks: []\n", "version is required"},
		"unsupported version":  {"version: 2\n", "unsupported version 2"},
		"version as string":    {"version: \"1\"\n", "invalid YAML"},
		"no deployment":        {"version: 1\nchecks: []\n", "deployment.name is required"},
		"blank name":           {"version: 1\ndeployment: {name: ' '}\n", "deployment.name must not be empty"},
		"control char name":    {"version: 1\ndeployment: {name: \"a\\nb\"}\n", "control"},
		"unknown top field":    {head + "  - {id: a, type: http, url: 'https://example.com/'}\nretries: 3\n", `unknown field "retries"`},
		"unknown check field":  {http("    expects: {status: 200}\n"), `unknown field "expects"`},
		"unknown expect field": {http("    expect: {statuss: 200}\n"), `unknown field "statuss"`},
		"no checks":            {head, "checks has no value"},
		"empty checks":         {"version: 1\ndeployment: {name: web}\nchecks: []\n", "at least one check"},
		"missing id":           {head + "  - {type: http, url: 'https://example.com/'}\n", "id is required"},
		"bad id":               {head + "  - {id: 'a b', type: http, url: 'https://example.com/'}\n", "id is required"},
		"duplicate id":         {head + "  - {id: a, type: http, url: 'https://example.com/'}\n  - {id: a, type: http, url: 'https://example.com/'}\n", `check "a": duplicate id`},
		"no type":              {head + "  - {id: a, url: 'https://example.com/'}\n", "type is required"},
		"unknown type":         {head + "  - {id: a, type: grpc}\n", `unknown type "grpc"`},
		"no url":               {head + "  - {id: a, type: http}\n", "url is required"},
		"bad scheme":           {head + "  - {id: a, type: http, url: 'ftp://example.com/'}\n", "http:// or https://"},
		"credentials in url":   {head + "  - {id: a, type: http, url: 'https://u:p@example.com/'}\n", "credentials"},
		"bad status":           {http("    expect: {status: 700}\n"), "between 100 and 599"},
		"octal status":         {http("    expect: {status: 0200}\n"), "decimal integer"},
		"hex status":           {http("    expect: {status: 0xC8}\n"), "decimal integer"},
		"string status":        {http("    expect: {status: '200'}\n"), "decimal integer"},
		"bad timeout":          {http("    timeout: 90s\n"), "between 1s and 60s"},
		"timeout no unit":      {http("    timeout: 10\n"), "between 1s and 60s"},
		"postgres field":       {http("    key: x\n"), `field "key" is not valid for type http`},
		"http field":           {pg("    url: https://example.com/\n    contract: checks/deploy.json\n    key: k\n"), `field "url" is not valid for type postgres`},
		"no contract":          {pg("    key: k\n"), "contract is required"},
		"no key":               {pg("    contract: checks/deploy.json\n"), "key is required"},
		"blank key":            {pg("    contract: checks/deploy.json\n    key: ' '\n"), "key must not be empty"},
		"missing contract":     {pg("    contract: checks/nope.json\n    key: k\n"), "file not found"},
		"absolute contract":    {pg("    contract: /etc/passwd\n    key: k\n"), "must be relative"},
		"escaping contract":    {pg("    contract: ../outside.json\n    key: k\n"), "inside the configuration file's directory"},
		"unset variable":       {"version: 1\ndeployment: {name: web, revision: '${NOPE}'}\n", "deployment.revision: environment variable NOPE is not set"},
		"empty variable":       {"version: 1\ndeployment: {name: web, revision: '${EMPTY}'}\n", "environment variable EMPTY is empty"},
		"empty revision":       {"version: 1\ndeployment: {name: web, revision: ''}\n", "deployment.revision must not be empty"},
		"empty key variable":   {pg("    contract: checks/deploy.json\n    key: '${EMPTY}'\n"), "key: environment variable EMPTY is empty"},
		"bare dollar":          {http("    expect: {contains: 'cost $5'}\n"), `write "$$"`},
		"unterminated":         {http("    expect: {contains: '${DEPLOY_SHA'}\n"), "variable reference"},
		"shell syntax":         {http("    expect: {contains: '${DEPLOY_SHA:-main}'}\n"), "invalid variable reference"},
		"command substitution": {http("    expect: {contains: '$(whoami)'}\n"), "variable reference"},
		"variable in name":     {"version: 1\ndeployment: {name: web}\nchecks:\n  - {id: '${X}', type: http}\n", "id is required"},
		"anchor":               {head + "  - &c {id: a, type: http, url: 'https://example.com/'}\n  - *c\n", "anchors and aliases"},
		"explicit tag":         {head + "  - {id: !!str a, type: http, url: 'https://example.com/'}\n", "explicit YAML tags"},
		"two documents":        {head + "  - {id: a, type: http, url: 'https://example.com/'}\n---\nversion: 1\n", "single YAML document"},
		"duplicate key":        {"version: 1\nversion: 1\n", "invalid YAML"},
		"null contains":        {http("    expect:\n      contains: null\n"), "contains has no value"},
		"missing contains":     {http("    expect:\n      status: 200\n      contains:\n"), "contains has no value"},
		"empty contains":       {http("    expect: {contains: ''}\n"), "expect.contains must not be empty"},
		"null expect":          {http("    expect:\n"), "expect has no value"},
		"empty expect":         {http("    expect: {}\n"), "expect must contain at least one assertion"},
		"null status":          {http("    expect: {status: ~}\n"), "status has no value"},
		"null key":             {pg("    contract: checks/deploy.json\n    key:\n"), "key has no value"},
		"merge key":            {http("    expect:\n      <<: {status: 201}\n      contains: x\n"), "merge keys"},
		"float version":        {"version: 1.0\n", "decimal integer"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.doc), dirWithContract(t), testEnv)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "config.raw") {
				t.Fatalf("error leaks Go type names: %v", err)
			}
		})
	}
}

func TestPostgresContractValidated(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"version":1,"table":"t; DROP TABLE t","key_column":"k","expect":{"a":"b"}}`), 0o600)
	os.WriteFile(filepath.Join(dir, "typo.json"), []byte(`{"version":1,"table":"t","key_column":"k","expects":{"a":"b"}}`), 0o600)
	for _, name := range []string{"bad.json", "typo.json"} {
		doc := "version: 1\ndeployment: {name: web}\nchecks:\n  - {id: db, type: postgres, contract: " + name + ", key: k}\n"
		if _, err := Parse([]byte(doc), dir, testEnv); err == nil || !strings.Contains(err.Error(), "contract "+name) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestSymlinkEscapeRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "c.json"), []byte(`{"version":1,"table":"t","key_column":"k","expect":{"a":"b"}}`), 0o600)
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "c.json"), filepath.Join(dir, "link.json")); err != nil {
		t.Fatal(err)
	}
	doc := "version: 1\ndeployment: {name: web}\nchecks:\n  - {id: db, type: postgres, contract: link.json, key: k}\n"
	if _, err := Parse([]byte(doc), dir, testEnv); err == nil || !strings.Contains(err.Error(), "inside the configuration") {
		t.Fatalf("symlink escape: %v", err)
	}
}

// Substituted values may be secrets; errors must never contain them.
func TestErrorsNeverContainSubstitutedValues(t *testing.T) {
	docs := []string{
		"version: 1\ndeployment: {name: web}\nchecks:\n  - {id: a, type: http, url: 'ftp://h/${TOKEN}'}\n",
		"version: 1\ndeployment: {name: web}\nchecks:\n  - {id: a, type: http, url: 'https://u:${TOKEN}@h/'}\n",
		"version: 1\ndeployment: {name: web}\nchecks:\n  - {id: a, type: http, url: 'https://h/${TOKEN}#x'}\n",
		"version: 1\ndeployment: {name: web, revision: \"${TOKEN}\"}\nchecks:\n  - {id: a, type: grpc}\n",
	}
	for _, doc := range docs {
		_, err := Parse([]byte(doc), t.TempDir(), testEnv)
		if err == nil || strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%q: %v", doc, err)
		}
	}
}

func TestExpand(t *testing.T) {
	cases := map[string]string{
		"plain":                        "plain",
		"${DEPLOY_SHA}":                "a82f19c",
		"v=${DEPLOY_SHA}-${DEPLOY_ID}": "v=a82f19c-run-1001",
		"$$5":                          "$5",
		"$${DEPLOY_SHA}":               "${DEPLOY_SHA}",
	}
	for in, want := range cases {
		got, err := expand(in, "f", testEnv)
		if err != nil || got != want {
			t.Errorf("expand(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	// Substituted values are not expanded again.
	got, err := expand("${A}", "f", env(map[string]string{"A": "${B}", "B": "x"}))
	if err != nil || got != "${B}" {
		t.Errorf("re-expansion: %q %v", got, err)
	}
}

func TestLoad(t *testing.T) {
	dir := dirWithContract(t)
	p := filepath.Join(dir, "aftergreen.yaml")
	os.WriteFile(p, []byte(valid), 0o600)
	if _, err := Load(p, testEnv); err != nil {
		t.Fatal(err)
	}
	// Relative contract paths resolve against the file, not the working directory.
	t.Chdir(t.TempDir())
	if _, err := Load(p, testEnv); err != nil {
		t.Fatalf("from another directory: %v", err)
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml"), testEnv); !os.IsNotExist(err) {
		t.Fatalf("missing file: %v", err)
	}
	big := filepath.Join(dir, "big.yaml")
	os.WriteFile(big, []byte("# "+strings.Repeat("x", MaxFileBytes)), 0o600)
	if _, err := Load(big, testEnv); err == nil || !strings.Contains(err.Error(), "64 KiB") {
		t.Fatalf("big file: %v", err)
	}
}
