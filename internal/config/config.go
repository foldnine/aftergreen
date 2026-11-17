// Package config loads and validates verification contracts (aftergreen.yaml)
// for `aftergreen verify`. See docs/verify.md for the format.
//
// Loading either returns a fully validated File, with environment variables
// already substituted, or an error. Nothing is checked over the network here.
// Errors name files, check IDs, fields and environment variable names. They
// never contain substituted values, which may be secrets.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"go.yaml.in/yaml/v3"

	"github.com/foldnine/aftergreen/internal/httpcheck"
	"github.com/foldnine/aftergreen/internal/postgres"
)

const (
	// SupportedVersion is the only contract format version this build reads.
	SupportedVersion = 1
	// MaxFileBytes bounds the configuration file size.
	MaxFileBytes = 64 << 10
	// MaxChecks bounds the number of checks in one file.
	MaxChecks = 64
	// DefaultTimeout applies to a check that does not set timeout.
	DefaultTimeout = 10 * time.Second

	// Retry bounds.
	DefaultRetryInterval = 5 * time.Second
	MinRetryInterval     = time.Second
	MaxRetryInterval     = 5 * time.Minute
	MaxRetryDeadline     = 30 * time.Minute
	MaxConsecutive       = 10

	TypeHTTP     = "http"
	TypePostgres = "postgres"
)

// File is a validated verification contract.
type File struct {
	Version    int
	Deployment Deployment
	Checks     []Check
	// UsesPostgres reports whether any check needs a PostgreSQL connection.
	UsesPostgres bool
}

// Deployment identifies what is being verified. Both values are printed in
// reports, so they must not be secret.
type Deployment struct {
	Name     string
	Revision string // empty when not configured
}

// Check is one validated check. Exactly one of HTTP and Postgres is set,
// matching Type.
type Check struct {
	ID      string
	Type    string
	Timeout time.Duration
	// Retry is nil for a single observation.
	Retry *Retry
	HTTP  *httpcheck.Contract
	// Postgres has its KeyValue set from the configuration's key, never from
	// the contract file.
	Postgres *postgres.Contract
}

// Retry repeats a check's observation until it is stable or the deadline
// passes. See internal/runner for the exact semantics.
type Retry struct {
	Interval    time.Duration // pause between observations
	Deadline    time.Duration // hard limit for the whole check, all attempts included
	Consecutive int           // VERIFIED observations in a row required
}

// LookupFunc resolves an environment variable, like os.LookupEnv.
type LookupFunc func(string) (string, bool)

// raw* mirror the YAML document. Pointers distinguish "absent" from "empty".
type rawFile struct {
	Version    *decimal       `yaml:"version"`
	Deployment *rawDeployment `yaml:"deployment"`
	Checks     []rawCheck     `yaml:"checks"`
}

type rawDeployment struct {
	Name     *string `yaml:"name"`
	Revision *string `yaml:"revision"`
}

type rawCheck struct {
	ID      *string   `yaml:"id"`
	Type    *string   `yaml:"type"`
	Timeout *string   `yaml:"timeout"`
	Retry   *rawRetry `yaml:"retry"`
	// http
	URL    *string    `yaml:"url"`
	Expect *rawExpect `yaml:"expect"`
	// postgres
	Contract *string `yaml:"contract"`
	Key      *string `yaml:"key"`
}

type rawExpect struct {
	Status   *decimal  `yaml:"status"`
	Contains *string   `yaml:"contains"`
	JSON     []rawJSON `yaml:"json"`
}

type rawJSON struct {
	Path   *string   `yaml:"path"`
	Equals yaml.Node `yaml:"equals"` // Kind 0 when absent; typed below
}

type rawRetry struct {
	Interval    *string  `yaml:"interval"`
	Deadline    *string  `yaml:"deadline"`
	Consecutive *decimal `yaml:"consecutive"`
}

// decimal is an integer written in plain decimal. YAML would otherwise read
// 0200 as octal (128) or accept 0xC8.
type decimal int

func (d *decimal) UnmarshalYAML(n *yaml.Node) error {
	v, err := strconv.Atoi(n.Value)
	if n.Kind != yaml.ScalarNode || n.Tag != "!!int" || err != nil || strconv.Itoa(v) != n.Value {
		return fmt.Errorf("line %d: expected a decimal integer", n.Line)
	}
	*d = decimal(v)
	return nil
}

var (
	checkID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// yaml.v3 names Go types in some errors; they mean nothing to users.
	goTypeSuffix = regexp.MustCompile(` (in|into) (type )?[*\[\]]*config\.\w+`)
	unknownField = regexp.MustCompile(`field (\S+) not found`)
)

// Load reads, validates and resolves a contract file. Relative contract paths
// inside it are resolved against the file's directory.
func Load(path string, lookup LookupFunc) (*File, error) {
	h, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer h.Close()
	b, err := io.ReadAll(io.LimitReader(h, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFileBytes {
		return nil, fmt.Errorf("%s is larger than 64 KiB", path)
	}
	return Parse(b, filepath.Dir(path), lookup)
}

// Parse validates a contract document. baseDir is the directory that relative
// contract paths are resolved against; contract files must stay inside it.
func Parse(b []byte, baseDir string, lookup LookupFunc) (*File, error) {
	raw, err := decode(b)
	if err != nil {
		return nil, err
	}
	return resolve(raw, baseDir, lookup)
}

func decode(b []byte) (*rawFile, error) {
	// First pass: structural restrictions that yaml.v3 cannot enforce while
	// decoding into structs.
	var doc yaml.Node
	dec := yaml.NewDecoder(bytes.NewReader(b))
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("configuration is empty")
		}
		return nil, yamlError(err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("configuration must be a single YAML document")
	}
	if err := checkNodes(&doc); err != nil {
		return nil, err
	}
	// Second pass: strict decoding. Unknown fields are errors, so a typo can
	// never silently drop an assertion.
	var raw rawFile
	dec = yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, yamlError(err)
	}
	return &raw, nil
}

// checkNodes rejects anchors, aliases and explicit tags. They are not needed
// for a flat contract and aliases enable resource-exhaustion documents.
func checkNodes(n *yaml.Node) error {
	if n.Kind == yaml.AliasNode || n.Anchor != "" {
		return fmt.Errorf("line %d: YAML anchors and aliases are not supported", n.Line)
	}
	if n.Style&yaml.TaggedStyle != 0 {
		return fmt.Errorf("line %d: explicit YAML tags are not supported", n.Line)
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Tag == "!!merge" {
				return fmt.Errorf("line %d: YAML merge keys (<<) are not supported", k.Line)
			}
			// "contains:" with no value would otherwise decode as absent and
			// silently drop the assertion.
			if v.Kind == yaml.ScalarNode && v.Tag == "!!null" && !nullAllowed[k.Value] {
				return fmt.Errorf("line %d: %s has no value (remove the field or give it a value)", k.Line, k.Value)
			}
		}
	}
	for _, c := range n.Content {
		if err := checkNodes(c); err != nil {
			return err
		}
	}
	return nil
}

// nullAllowed lists fields whose value may be YAML null.
var nullAllowed = map[string]bool{"equals": true}

func yamlError(err error) error {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	msg = strings.ReplaceAll(msg, "unmarshal errors:\n  ", "")
	msg = goTypeSuffix.ReplaceAllString(msg, "")
	msg = unknownField.ReplaceAllString(msg, `unknown field "$1"`)
	return errors.New("invalid YAML: " + msg)
}

func resolve(raw *rawFile, baseDir string, lookup LookupFunc) (*File, error) {
	if raw.Version == nil {
		return nil, fmt.Errorf("version is required (supported: %d)", SupportedVersion)
	}
	if int(*raw.Version) != SupportedVersion {
		return nil, fmt.Errorf("unsupported version %d (this aftergreen supports version %d)", *raw.Version, SupportedVersion)
	}
	out := &File{Version: int(*raw.Version)}

	if raw.Deployment == nil || raw.Deployment.Name == nil {
		return nil, errors.New("deployment.name is required")
	}
	name := *raw.Deployment.Name
	if err := printable("deployment.name", name, 100); err != nil {
		return nil, err
	}
	out.Deployment.Name = name
	if raw.Deployment.Revision != nil {
		rev, err := expand(*raw.Deployment.Revision, "deployment.revision", lookup)
		if err != nil {
			return nil, err
		}
		if err := printable("deployment.revision", rev, 200); err != nil {
			return nil, err
		}
		out.Deployment.Revision = rev
	}

	if len(raw.Checks) == 0 {
		return nil, errors.New("checks: at least one check is required")
	}
	if len(raw.Checks) > MaxChecks {
		return nil, fmt.Errorf("checks: at most %d checks are supported", MaxChecks)
	}
	seen := map[string]bool{}
	for i, rc := range raw.Checks {
		if rc.ID == nil || !checkID.MatchString(*rc.ID) {
			return nil, fmt.Errorf("checks[%d]: id is required: 1-64 letters, digits, '.', '_' or '-', starting with a letter or digit", i)
		}
		id := *rc.ID
		if seen[id] {
			return nil, fmt.Errorf("check %q: duplicate id", id)
		}
		seen[id] = true
		c, err := resolveCheck(rc, baseDir, lookup)
		if err != nil {
			return nil, fmt.Errorf("check %q: %w", id, err)
		}
		if c.Type == TypePostgres {
			out.UsesPostgres = true
		}
		out.Checks = append(out.Checks, c)
	}
	return out, nil
}

func resolveCheck(rc rawCheck, baseDir string, lookup LookupFunc) (Check, error) {
	c := Check{ID: *rc.ID, Timeout: DefaultTimeout}
	if rc.Type == nil {
		return c, fmt.Errorf("type is required (supported: %s, %s)", TypeHTTP, TypePostgres)
	}
	c.Type = *rc.Type
	if rc.Timeout != nil {
		d, err := time.ParseDuration(*rc.Timeout)
		if err != nil || d < httpcheck.MinTimeout || d > httpcheck.MaxTimeout {
			return c, errors.New(`timeout must be a duration between 1s and 60s, e.g. "10s"`)
		}
		c.Timeout = d
	}
	if rc.Retry != nil {
		r, err := resolveRetry(*rc.Retry, c.Timeout)
		if err != nil {
			return c, err
		}
		c.Retry = r
	}
	switch c.Type {
	case TypeHTTP:
		if err := onlyFields(c.Type, map[string]bool{"contract": rc.Contract != nil, "key": rc.Key != nil}); err != nil {
			return c, err
		}
		h, err := resolveHTTP(rc, c.Timeout, lookup)
		c.HTTP = h
		return c, err
	case TypePostgres:
		if err := onlyFields(c.Type, map[string]bool{"url": rc.URL != nil, "expect": rc.Expect != nil}); err != nil {
			return c, err
		}
		p, err := resolvePostgres(rc, baseDir, lookup)
		c.Postgres = p
		return c, err
	default:
		return c, fmt.Errorf("unknown type %q (supported: %s, %s)", c.Type, TypeHTTP, TypePostgres)
	}
}

func onlyFields(typ string, present map[string]bool) error {
	for _, name := range []string{"url", "expect", "contract", "key"} {
		if present[name] {
			return fmt.Errorf("field %q is not valid for type %s", name, typ)
		}
	}
	return nil
}

func resolveHTTP(rc rawCheck, timeout time.Duration, lookup LookupFunc) (*httpcheck.Contract, error) {
	if rc.URL == nil {
		return nil, errors.New("url is required")
	}
	u, err := expand(*rc.URL, "url", lookup)
	if err != nil {
		return nil, err
	}
	h := &httpcheck.Contract{URL: u, ExpectedStatus: 200, Timeout: timeout}
	if rc.Expect != nil {
		if rc.Expect.Status == nil && rc.Expect.Contains == nil && rc.Expect.JSON == nil {
			return nil, errors.New("expect must contain at least one assertion (or remove it to check status 200)")
		}
		if rc.Expect.Status != nil {
			h.ExpectedStatus = int(*rc.Expect.Status)
		}
		if rc.Expect.Contains != nil {
			s, err := expand(*rc.Expect.Contains, "expect.contains", lookup)
			if err != nil {
				return nil, err
			}
			if s == "" {
				return nil, errors.New("expect.contains must not be empty")
			}
			h.ExpectedBodySubstring = s
		}
		if rc.Expect.JSON != nil && len(rc.Expect.JSON) == 0 {
			return nil, errors.New("expect.json must list at least one assertion")
		}
		for i, rj := range rc.Expect.JSON {
			a, err := resolveJSON(rj, i, lookup)
			if err != nil {
				return nil, err
			}
			h.JSON = append(h.JSON, a)
		}
	}
	// Validate never includes the URL in its errors.
	if _, err := h.Validate(); err != nil {
		return nil, err
	}
	return h, nil
}

// resolveJSON converts one assertion. equals keeps its YAML type: "3" is a
// string, 3 a number, true a boolean, null null. ${VAR} is substituted in
// strings only.
func resolveJSON(rj rawJSON, i int, lookup LookupFunc) (httpcheck.JSONAssertion, error) {
	field := fmt.Sprintf("expect.json[%d]", i)
	if rj.Path == nil {
		return httpcheck.JSONAssertion{}, fmt.Errorf("%s.path is required", field)
	}
	a := httpcheck.JSONAssertion{Path: *rj.Path}
	n := rj.Equals
	if n.Kind == 0 {
		return a, fmt.Errorf("%s.equals is required", field)
	}
	if n.Kind != yaml.ScalarNode {
		return a, fmt.Errorf("%s.equals must be a string, number, boolean or null", field)
	}
	switch n.Tag {
	case "!!str":
		v, err := expand(n.Value, field+".equals", lookup)
		if err != nil {
			return a, err
		}
		a.Equals = v
	case "!!int", "!!float":
		a.Equals = json.Number(n.Value) // JSON number syntax is checked by httpcheck
	case "!!bool":
		a.Equals = n.Value == "true"
		if n.Value != "true" && n.Value != "false" {
			return a, fmt.Errorf("%s.equals: write booleans as true or false", field)
		}
	case "!!null":
		if n.Value != "null" {
			return a, fmt.Errorf("%s.equals: write null as null", field)
		}
		a.Equals = nil
	default:
		return a, fmt.Errorf("%s.equals must be a string, number, boolean or null", field)
	}
	return a, nil
}

func resolveRetry(rr rawRetry, timeout time.Duration) (*Retry, error) {
	r := &Retry{Interval: DefaultRetryInterval, Consecutive: 1}
	if rr.Interval != nil {
		d, err := time.ParseDuration(*rr.Interval)
		if err != nil || d < MinRetryInterval || d > MaxRetryInterval {
			return nil, errors.New(`retry.interval must be a duration between 1s and 5m, e.g. "5s"`)
		}
		r.Interval = d
	}
	if rr.Deadline == nil {
		return nil, errors.New(`retry.deadline is required: the longest time to keep trying, e.g. "2m"`)
	}
	d, err := time.ParseDuration(*rr.Deadline)
	if err != nil || d < timeout || d > MaxRetryDeadline {
		return nil, fmt.Errorf("retry.deadline must be a duration between the check timeout (%s) and 30m", timeout)
	}
	r.Deadline = d
	if rr.Consecutive != nil {
		n := int(*rr.Consecutive)
		if n < 1 || n > MaxConsecutive {
			return nil, fmt.Errorf("retry.consecutive must be between 1 and %d", MaxConsecutive)
		}
		r.Consecutive = n
	}
	// n consecutive observations need at least (n-1) intervals.
	if time.Duration(r.Consecutive-1)*r.Interval >= r.Deadline {
		return nil, fmt.Errorf("retry.deadline (%s) is too short for %d consecutive observations %s apart", r.Deadline, r.Consecutive, r.Interval)
	}
	return r, nil
}

func resolvePostgres(rc rawCheck, baseDir string, lookup LookupFunc) (*postgres.Contract, error) {
	if rc.Contract == nil || *rc.Contract == "" {
		return nil, errors.New("contract is required (path to a PostgreSQL contract JSON file)")
	}
	// The key identifies this deployment's row. It must come from the
	// configuration: falling back to a key stored in the contract file could
	// verify a stale row from an earlier deployment.
	if rc.Key == nil {
		return nil, errors.New(`key is required (usually "${DEPLOY_ID}" or similar)`)
	}
	key, err := expand(*rc.Key, "key", lookup)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("key must not be empty")
	}
	b, err := readContained(baseDir, *rc.Contract)
	if err != nil {
		return nil, err
	}
	file, err := postgres.Parse(b)
	if err != nil {
		return nil, fmt.Errorf("contract %s: %w", *rc.Contract, err)
	}
	contract := file.Contract(key)
	if _, _, err := contract.Statement(); err != nil {
		return nil, fmt.Errorf("contract %s: %w", *rc.Contract, err)
	}
	return &contract, nil
}

// readContained reads a relative path inside baseDir. os.Root also refuses
// symlinks and ".." components that would leave the directory.
func readContained(baseDir, rel string) ([]byte, error) {
	if filepath.IsAbs(rel) || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) {
		return nil, fmt.Errorf("contract %s: path must be relative to the configuration file", rel)
	}
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return nil, fmt.Errorf("contract %s: %w", rel, err)
	}
	defer root.Close()
	h, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("contract %s: file not found (paths are relative to the configuration file)", rel)
		}
		return nil, fmt.Errorf("contract %s: cannot open: path must stay inside the configuration file's directory", rel)
	}
	defer h.Close()
	b, err := io.ReadAll(io.LimitReader(h, postgres.MaxContractBytes+1))
	if err != nil {
		return nil, fmt.Errorf("contract %s: %w", rel, err)
	}
	return b, nil
}

// printable rejects empty values, control characters and overly long values
// for fields that are printed in reports.
func printable(field, v string, max int) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%s must not be empty", field)
	}
	if len(v) > max {
		return fmt.Errorf("%s is longer than %d bytes", field, max)
	}
	for _, r := range v {
		if unicode.IsControl(r) || r == unicode.ReplacementChar {
			return fmt.Errorf("%s contains control or invalid characters", field)
		}
	}
	return nil
}

// expand substitutes ${NAME} references. "$$" is a literal "$"; any other
// use of "$" is an error. A referenced variable that is unset or empty is an
// error: an empty deploy ID must never turn into a check that passes against
// an old deployment. Substituted text is not expanded again.
func expand(s, field string, lookup LookupFunc) (string, error) {
	if !strings.Contains(s, "$") {
		return s, nil
	}
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			out.WriteByte(s[i])
			continue
		}
		if i+1 < len(s) && s[i+1] == '$' {
			out.WriteByte('$')
			i++
			continue
		}
		end := strings.IndexByte(s[i:], '}')
		if i+1 >= len(s) || s[i+1] != '{' || end < 0 {
			return "", fmt.Errorf(`%s: "$" must start a ${NAME} variable reference (write "$$" for a literal "$")`, field)
		}
		name := s[i+2 : i+end]
		if !envName.MatchString(name) {
			return "", fmt.Errorf("%s: invalid variable reference: names use letters, digits and underscores", field)
		}
		v, ok := lookup(name)
		if !ok {
			return "", fmt.Errorf("%s: environment variable %s is not set", field, name)
		}
		if v == "" {
			return "", fmt.Errorf("%s: environment variable %s is empty", field, name)
		}
		out.WriteString(v)
		i += end
	}
	return out.String(), nil
}
