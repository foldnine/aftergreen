// Package httpcheck verifies HTTP outcomes without treating transport errors as failed assertions.
//
// Verdicts:
//   - VERIFIED: a response was observed and every assertion holds.
//   - FAILED:   a response was observed and at least one assertion is false.
//     A wrong status is FAILED even if the body cannot be read.
//   - UNKNOWN:  the outcome could not be observed or trusted (transport error,
//     timeout, truncated body, body over the size limit).
//
// Invalid contracts return an error and no verdict. Behaviour is pinned by the
// shared cases in conformance/http/cases.json, which the GitHub Action also runs.
package httpcheck

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxBodyBytes is the largest response body inspected for a body assertion.
	MaxBodyBytes = 1 << 20
	MinTimeout   = time.Second
	MaxTimeout   = 60 * time.Second
)

// UserAgent is sent with every request so operators can identify AfterGreen checks.
var UserAgent = "aftergreen"

const (
	StatusVerified = "VERIFIED"
	StatusFailed   = "FAILED"
	StatusUnknown  = "UNKNOWN"
)

type Contract struct {
	URL                   string
	ExpectedStatus        int
	ExpectedBodySubstring string
	// JSON assertions on the response body (used by `aftergreen verify`).
	// When set, a body that is not valid JSON is FAILED.
	JSON []JSONAssertion
	// Timeout bounds the whole request including the body read; 1s..60s.
	Timeout time.Duration
}

type Verdict struct {
	Status         string   `json:"status"`
	Reasons        []string `json:"reasons,omitempty"`
	ObservedStatus int      `json:"observed_status,omitempty"`
}

// Validate reports why a contract cannot be checked. Error messages never
// include the URL, which may carry a token in its query string.
func (c Contract) Validate() (*url.URL, error) {
	if strings.TrimSpace(c.URL) == "" {
		return nil, errors.New("URL is required")
	}
	parsed, err := url.Parse(c.URL)
	if err != nil || !parsed.IsAbs() {
		return nil, errors.New("URL must be an absolute http:// or https:// URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("URL must use http:// or https://")
	}
	if parsed.Hostname() == "" {
		return nil, errors.New("URL must include a host")
	}
	if port := parsed.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return nil, errors.New("URL port must be between 1 and 65535")
		}
	}
	if parsed.User != nil {
		return nil, errors.New("URL must not contain credentials; credentials in URLs leak into logs")
	}
	if parsed.Fragment != "" || strings.Contains(c.URL, "#") {
		return nil, errors.New("URL must not contain a #fragment; fragments are never sent to the server")
	}
	if c.ExpectedStatus < 100 || c.ExpectedStatus > 599 {
		return nil, errors.New("expected HTTP status must be between 100 and 599")
	}
	if c.Timeout < MinTimeout || c.Timeout > MaxTimeout {
		return nil, errors.New("timeout must be between 1s and 60s")
	}
	if err := validateAssertions(c.JSON); err != nil {
		return nil, err
	}
	return parsed, nil
}

func unknown(observed int, reason string) Verdict {
	return Verdict{Status: StatusUnknown, ObservedStatus: observed, Reasons: []string{reason}}
}

// Verify performs one GET request and evaluates the contract. A non-nil error
// means the contract is invalid and nothing was checked.
func Verify(ctx context.Context, contract Contract) (Verdict, error) {
	target, err := contract.Validate()
	if err != nil {
		return Verdict{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, contract.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return Verdict{}, errors.New("URL cannot be requested")
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", UserAgent)

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: contract.Timeout}).DialContext,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:   contract.Timeout,
		ResponseHeaderTimeout: contract.Timeout,
		ForceAttemptHTTP2:     true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		// Redirects are not followed: the requested destination must be the verified destination.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return unknown(0, "HTTP observation timed out"), nil
		}
		return unknown(0, "HTTP observation unavailable"), nil
	}
	defer response.Body.Close()

	observed := response.StatusCode
	verdict := Verdict{Status: StatusVerified, ObservedStatus: observed}
	statusMatches := observed == contract.ExpectedStatus
	if !statusMatches {
		verdict.Status = StatusFailed
		verdict.Reasons = append(verdict.Reasons, fmt.Sprintf("expected HTTP status %d, observed %d", contract.ExpectedStatus, observed))
	}
	if contract.ExpectedBodySubstring == "" && len(contract.JSON) == 0 {
		return verdict, nil
	}

	body, readErr := io.ReadAll(io.LimitReader(response.Body, MaxBodyBytes+1))
	switch {
	case !statusMatches && (readErr != nil || len(body) > MaxBodyBytes):
		// A false status assertion is already definitive.
		return verdict, nil
	case readErr != nil && ctx.Err() != nil:
		return unknown(observed, "HTTP response body read timed out"), nil
	case readErr != nil:
		return unknown(observed, "HTTP response body unreadable"), nil
	case len(body) > MaxBodyBytes:
		return unknown(observed, "HTTP response body exceeds the 1 MiB verification limit"), nil
	}
	if contract.ExpectedBodySubstring != "" && !bytes.Contains(body, []byte(contract.ExpectedBodySubstring)) {
		verdict.Status = StatusFailed
		verdict.Reasons = append(verdict.Reasons, "response body did not contain the expected text")
	}
	// JSON assertions describe the expected response; an error page with a
	// wrong status is already FAILED and is not parsed.
	if len(contract.JSON) > 0 && statusMatches {
		reasons, unknownReason := evaluateJSON(body, contract.JSON)
		if unknownReason != "" && verdict.Status != StatusFailed {
			return unknown(observed, unknownReason), nil
		}
		if len(reasons) > 0 {
			verdict.Status = StatusFailed
			verdict.Reasons = append(verdict.Reasons, reasons...)
		}
	}
	return verdict, nil
}

func (v Verdict) JSON() string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }
