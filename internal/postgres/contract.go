package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// File is the on-disk contract for `aftergreen postgres` (version 1).
//
//	{
//	  "version": 1,
//	  "schema": "public",
//	  "table": "leads",
//	  "key_column": "event_id",
//	  "key": "evt-2026-11-15-001",
//	  "expect": {"status": "qualified"}
//	}
//
// The connection string is never part of the file.
type File struct {
	Version   int               `json:"version"`
	Schema    string            `json:"schema"`
	Table     string            `json:"table"`
	KeyColumn string            `json:"key_column"`
	Key       string            `json:"key"`
	Expect    map[string]string `json:"expect"`
}

const maxContractBytes = 64 << 10

// LoadFile reads and strictly decodes a contract file. Unknown fields are
// rejected so a typo can never weaken a check.
func LoadFile(path string) (File, error) {
	var f File
	b, err := os.ReadFile(path)
	if err != nil {
		return f, err
	}
	if len(b) > maxContractBytes {
		return f, errors.New("contract file is larger than 64 KiB")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return f, fmt.Errorf("decode contract: %w", err)
	}
	if dec.More() {
		return f, errors.New("decode contract: unexpected data after the JSON document")
	}
	if f.Version != 1 {
		return f, fmt.Errorf("unsupported contract version %d (supported: 1)", f.Version)
	}
	return f, nil
}

// Contract converts the file to a verification contract; keyOverride, when
// non-empty, replaces the file's key (e.g. a deploy or event ID from CI).
func (f File) Contract(keyOverride string) Contract {
	key := f.Key
	if keyOverride != "" {
		key = keyOverride
	}
	return Contract{Schema: f.Schema, Table: f.Table, KeyColumn: f.KeyColumn, KeyValue: key, Expected: f.Expect}
}
