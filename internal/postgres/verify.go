// Package postgres verifies destination evidence using a read-only SQL transaction.
// Callers supply a PostgreSQL-backed database/sql connection with SELECT-only privileges.
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

var identifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

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

func quote(v string) (string, error) {
	if !identifier.MatchString(v) {
		return "", fmt.Errorf("invalid SQL identifier %q", v)
	}
	return `"` + v + `"`, nil
}

// Verify correlates an event with a destination row and compares source expectations.
// UNKNOWN indicates a verification outage, not a verified failure.
func Verify(ctx context.Context, db *sql.DB, c Contract) (Verdict, error) {
	if db == nil {
		return Verdict{}, errors.New("database is nil")
	}
	if c.KeyValue == "" {
		return Verdict{}, errors.New("event key is required")
	}
	if len(c.Expected) == 0 {
		return Verdict{}, errors.New("expectations are required")
	}
	schema := c.Schema
	if schema == "" {
		schema = "public"
	}
	qs, err := quote(schema)
	if err != nil {
		return Verdict{}, err
	}
	qt, err := quote(c.Table)
	if err != nil {
		return Verdict{}, err
	}
	qk, err := quote(c.KeyColumn)
	if err != nil {
		return Verdict{}, err
	}
	keys := make([]string, 0, len(c.Expected))
	for k := range c.Expected {
		if _, err = quote(k); err != nil {
			return Verdict{}, err
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cols := make([]string, 0, len(keys))
	for _, k := range keys {
		q, _ := quote(k)
		cols = append(cols, q+"::text")
	}
	statement := fmt.Sprintf("SELECT %s FROM %s.%s WHERE %s = $1 LIMIT 2", strings.Join(cols, ", "), qs, qt, qk)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Verdict{Status: "UNKNOWN", Reasons: []string{"cannot start read-only transaction"}}, nil
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, statement, c.KeyValue)
	if err != nil {
		return Verdict{Status: "UNKNOWN", Reasons: []string{"destination query unavailable"}}, nil
	}
	defer rows.Close()
	vals := make([]sql.NullString, len(keys))
	ptr := make([]any, len(keys))
	for i := range vals {
		ptr[i] = &vals[i]
	}
	if !rows.Next() {
		if rows.Err() != nil {
			return Verdict{Status: "UNKNOWN", Reasons: []string{"destination read interrupted"}}, nil
		}
		return Verdict{Status: "FAILED", Reasons: []string{"destination record missing"}}, nil
	}
	if err := rows.Scan(ptr...); err != nil {
		return Verdict{Status: "UNKNOWN", Reasons: []string{"destination decode failed"}}, nil
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
		return Verdict{Status: "FAILED", Reasons: []string{"multiple destination rows match event key"}}, nil
	}
	if rows.Err() != nil {
		return Verdict{Status: "UNKNOWN", Reasons: []string{"destination read interrupted"}}, nil
	}
	if len(reasons) > 0 {
		return Verdict{Status: "FAILED", Reasons: reasons}, nil
	}
	return Verdict{Status: "VERIFIED"}, nil
}
