package httpcheck

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"unicode/utf8"
)

// JSON response assertions.
//
// Path syntax (deliberately small): "$" is the whole document, ".name" selects
// an object member (name: letters, digits, "_" and "-", not starting with a
// digit or "-"), "[n]" selects an array element (n: decimal, from 0).
// Examples: $  $.revision  $.build.sha  $.instances[0].version
//
// Comparison is exact and type-aware: the string "1" never equals the number 1.
// Numbers compare by exact value, so 1, 1.0 and 1e0 are equal.

const (
	MaxJSONAssertions = 16
	maxPathLen        = 256
	maxPathSegments   = 32
	maxJSONDepth      = 64
)

// JSONAssertion requires the value at Path to equal Equals. Equals is nil
// (JSON null), a bool, a string, or a json.Number.
type JSONAssertion struct {
	Path   string
	Equals any
}

type pathSegment struct {
	name  string
	index int // used when name is ""
}

var (
	pathToken  = regexp.MustCompile(`^(?:\.([A-Za-z_][A-Za-z0-9_-]*)|\[(0|[1-9][0-9]{0,5})\])`)
	jsonNumber = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
)

func parsePath(p string) ([]pathSegment, error) {
	if len(p) > maxPathLen || len(p) == 0 || p[0] != '$' {
		return nil, errors.New(`JSON path must start with "$" (e.g. "$.revision")`)
	}
	var segs []pathSegment
	for rest := p[1:]; rest != ""; {
		m := pathToken.FindStringSubmatch(rest)
		if m == nil {
			return nil, fmt.Errorf(`JSON path %q: supported syntax is $, .name and [index]`, p)
		}
		if m[1] != "" {
			segs = append(segs, pathSegment{name: m[1]})
		} else {
			n, _ := strconv.Atoi(m[2])
			segs = append(segs, pathSegment{index: n})
		}
		rest = rest[len(m[0]):]
	}
	if len(segs) > maxPathSegments {
		return nil, fmt.Errorf("JSON path %q has more than %d segments", p, maxPathSegments)
	}
	return segs, nil
}

func validateAssertions(as []JSONAssertion) error {
	if len(as) > MaxJSONAssertions {
		return fmt.Errorf("at most %d JSON assertions are supported", MaxJSONAssertions)
	}
	for _, a := range as {
		if _, err := parsePath(a.Path); err != nil {
			return err
		}
		switch v := a.Equals.(type) {
		case nil, bool, string:
		case json.Number:
			if _, ok := new(big.Rat).SetString(string(v)); !ok || !jsonNumber.MatchString(string(v)) {
				return fmt.Errorf("JSON assertion %s: expected value is not a valid JSON number", a.Path)
			}
		default:
			return fmt.Errorf("JSON assertion %s: expected value must be a string, number, boolean or null", a.Path)
		}
	}
	return nil
}

var (
	errDuplicateKey = errors.New("duplicate object key")
	errTooDeep      = errors.New("nested too deeply")
)

// decodeStrict parses one JSON document: valid UTF-8, no trailing data, and
// no duplicate object keys (parsers disagree on which duplicate wins).
func decodeStrict(b []byte) (any, error) {
	if !utf8.Valid(b) {
		return nil, errors.New("invalid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	v, err := decodeValue(dec, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after the JSON document")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder, depth int) (any, error) {
	if depth > maxJSONDepth {
		return nil, errTooDeep
	}
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := map[string]any{}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				k := kt.(string) // the decoder guarantees a string key here
				if _, dup := obj[k]; dup {
					return nil, errDuplicateKey
				}
				if obj[k], err = decodeValue(dec, depth+1); err != nil {
					return nil, err
				}
			}
			_, err := dec.Token() // '}'
			return obj, err
		case '[':
			arr := []any{}
			for dec.More() {
				v, err := decodeValue(dec, depth+1)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			_, err := dec.Token() // ']'
			return arr, err
		}
		return nil, errors.New("unexpected delimiter")
	default:
		return t, nil // nil, bool, string, json.Number
	}
}

func lookup(doc any, segs []pathSegment) (any, bool) {
	cur := doc
	for _, s := range segs {
		if s.name != "" {
			obj, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			if cur, ok = obj[s.name]; !ok {
				return nil, false
			}
			continue
		}
		arr, ok := cur.([]any)
		if !ok || s.index >= len(arr) {
			return nil, false
		}
		cur = arr[s.index]
	}
	return cur, true
}

func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case json.Number:
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "unknown"
}

// numbersEqual compares exactly. ok is false when a number is too large to
// represent exactly (e.g. an exponent beyond big.Rat's range).
func numbersEqual(a, b json.Number) (equal, ok bool) {
	x, ok1 := new(big.Rat).SetString(string(a))
	y, ok2 := new(big.Rat).SetString(string(b))
	if !ok1 || !ok2 {
		return false, false
	}
	return x.Cmp(y) == 0, true
}

// evaluateJSON checks assertions against a fully read body. It returns the
// failed-assertion reasons, or unknown=true when the body cannot be trusted.
// Reasons name the path and types only, never values.
func evaluateJSON(body []byte, as []JSONAssertion) (reasons []string, unknownReason string) {
	doc, err := decodeStrict(body)
	if errors.Is(err, errDuplicateKey) {
		return nil, "response JSON has duplicate object keys; the value is ambiguous"
	}
	if errors.Is(err, errTooDeep) {
		return nil, fmt.Sprintf("response JSON is nested more than %d levels deep", maxJSONDepth)
	}
	if err != nil {
		return []string{"response body is not valid JSON"}, ""
	}
	for _, a := range as {
		segs, _ := parsePath(a.Path) // validated with the contract
		got, ok := lookup(doc, segs)
		if !ok {
			reasons = append(reasons, fmt.Sprintf("json %s: not found", a.Path))
			continue
		}
		want := a.Equals
		if typeName(got) != typeName(want) {
			reasons = append(reasons, fmt.Sprintf("json %s: expected %s, observed %s", a.Path, typeName(want), typeName(got)))
			continue
		}
		equal := false
		switch w := want.(type) {
		case nil:
			equal = true
		case bool:
			equal = got.(bool) == w
		case string:
			equal = got.(string) == w
		case json.Number:
			var ok bool
			if equal, ok = numbersEqual(got.(json.Number), w); !ok {
				return nil, fmt.Sprintf("json %s: number cannot be compared exactly", a.Path)
			}
		}
		if !equal {
			reasons = append(reasons, fmt.Sprintf("json %s: value differs", a.Path))
		}
	}
	return reasons, ""
}
