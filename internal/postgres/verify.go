// Package postgres verifies destination evidence using a read-only SQL transaction.
// Callers supply a PostgreSQL-backed database/sql connection with SELECT-only privileges.
//
// Only one statement shape is ever executed:
//
//	SELECT "<field>"::text, ... FROM "<schema>"."<table>" WHERE "<key_column>" = $1 LIMIT 2
//
// Identifiers are validated and quoted. The event key is a bound parameter. No
// SQL text is ever accepted from configuration.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// identifier accepts plain PostgreSQL identifiers (max 63 bytes, NAMEDATALEN-1).
// Quoting preserves case, so names must match the catalog exactly (usually lower case).
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

const (
	StatusVerified = "VERIFIED"
	StatusFailed   = "FAILED"
	StatusUnknown  = "UNKNOWN"
)

type Contract struct {
	Schema    string
	Table     string
	KeyColumn string
	KeyValue  string
	Expected  map[string]string
}

type Verdict struct {
	Status  string   `json:"status"`
	Reasons []string `json:"reasons,omitempty"`
}

func quote(kind, v string) (string, error) {
	if !identifier.MatchString(v) {
		// The value is shown on purpose: it comes from the contract author, not from the database.
		return "", fmt.Errorf("invalid %s %q: use letters, digits and underscores (max 63), starting with a letter or underscore", kind, v)
	}
	return `"` + v + `"`, nil
}

// Statement validates the contract and returns the exact SQL that Verify runs.
func (c Contract) Statement() (string, []string, error) {
	if c.KeyValue == "" {
		return "", nil, errors.New("event key is required")
	}
	if len(c.Expected) == 0 {
		return "", nil, errors.New("at least one expected field is required")
	}
	schema := c.Schema
	if schema == "" {
		schema = "public"
	}
	qs, err := quote("schema", schema)
	if err != nil {
		return "", nil, err
	}
	qt, err := quote("table", c.Table)
	if err != nil {
		return "", nil, err
	}
	qk, err := quote("key column", c.KeyColumn)
	if err != nil {
		return "", nil, err
	}
	keys := make([]string, 0, len(c.Expected))
	for k := range c.Expected {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cols := make([]string, 0, len(keys))
	for _, k := range keys {
		q, err := quote("field", k)
		if err != nil {
			return "", nil, err
		}
		cols = append(cols, q+"::text")
	}
	return fmt.Sprintf("SELECT %s FROM %s.%s WHERE %s = $1 LIMIT 2", strings.Join(cols, ", "), qs, qt, qk), keys, nil
}

// unavailable builds an UNKNOWN verdict. Only the SQLSTATE code is reported:
// server error messages can contain row values.
func unavailable(what string, err error) Verdict {
	reason := what
	var coded interface{ SQLState() string }
	if errors.As(err, &coded) && coded.SQLState() != "" {
		reason += " (SQLSTATE " + coded.SQLState() + hint(coded.SQLState()) + ")"
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		reason += " (timed out or cancelled)"
	}
	return Verdict{Status: StatusUnknown, Reasons: []string{reason}}
}

func hint(code string) string {
	switch {
	case code == "42501":
		return ": insufficient privilege"
	case code == "42P01":
		return ": table not found"
	case code == "42703":
		return ": column not found"
	case code == "57014":
		return ": query cancelled"
	case code == "22P02":
		return ": event key does not fit the key column type"
	case strings.HasPrefix(code, "08"):
		return ": connection exception"
	case strings.HasPrefix(code, "28"):
		return ": authentication failed"
	}
	return ""
}

// Verify correlates an event with a destination row and compares source expectations.
// A non-nil error means the contract is invalid; UNKNOWN means the destination
// could not be observed, which is never a pass.
func Verify(ctx context.Context, db *sql.DB, c Contract) (Verdict, error) {
	if db == nil {
		return Verdict{}, errors.New("database is nil")
	}
	statement, keys, err := c.Statement()
	if err != nil {
		return Verdict{}, err
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return unavailable("cannot start read-only transaction", err), nil
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, statement, c.KeyValue)
	if err != nil {
		return unavailable("destination query failed", err), nil
	}
	defer rows.Close()
	vals := make([]sql.NullString, len(keys))
	ptr := make([]any, len(keys))
	for i := range vals {
		ptr[i] = &vals[i]
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return unavailable("destination read interrupted", err), nil
		}
		return Verdict{Status: StatusFailed, Reasons: []string{"destination record missing"}}, nil
	}
	if err := rows.Scan(ptr...); err != nil {
		return unavailable("destination decode failed", err), nil
	}
	reasons := []string{}
	for i, k := range keys {
		if !vals[i].Valid {
			reasons = append(reasons, fmt.Sprintf("%s: NULL", k))
		} else if vals[i].String != c.Expected[k] {
			reasons = append(reasons, fmt.Sprintf("%s: value mismatch", k))
		}
	}
	if rows.Next() {
		return Verdict{Status: StatusFailed, Reasons: []string{"multiple destination rows match event key"}}, nil
	}
	if err := rows.Err(); err != nil {
		return unavailable("destination read interrupted", err), nil
	}
	if len(reasons) > 0 {
		return Verdict{Status: StatusFailed, Reasons: reasons}, nil
	}
	return Verdict{Status: StatusVerified}, nil
}
