// Package httpcheck verifies HTTP outcomes without treating transport errors as failed assertions.
package httpcheck

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxResponseBytes = 1 << 20

type Contract struct {
	URL                   string
	ExpectedStatus        int
	ExpectedBodySubstring string
	Timeout               time.Duration
}
type Verdict struct {
	Status         string   `json:"status"`
	Reasons        []string `json:"reasons,omitempty"`
	ObservedStatus int      `json:"observed_status,omitempty"`
}

func Verify(ctx context.Context, contract Contract) (Verdict, error) {
	parsed, err := url.Parse(contract.URL)
	if err != nil {
		return Verdict{}, fmt.Errorf("invalid URL: %w", err)
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return Verdict{}, errors.New("URL must be http(s), include a host, and must not include credentials or fragments")
	}
	if contract.ExpectedStatus < 100 || contract.ExpectedStatus > 599 {
		return Verdict{}, errors.New("expected HTTP status must be 100..599")
	}
	if contract.Timeout <= 0 {
		contract.Timeout = 10 * time.Second
	}
	if contract.Timeout > 60*time.Second {
		return Verdict{}, errors.New("timeout may not exceed 60 seconds")
	}
	ctx, cancel := context.WithTimeout(ctx, contract.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return Verdict{}, fmt.Errorf("request: %w", err)
	}
	// Redirects are not followed: the requested destination must be the verified destination.
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyFromEnvironment,
			DialContext:     (&net.Dialer{Timeout: contract.Timeout}).DialContext,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			MaxIdleConns:    2,
			IdleConnTimeout: 5 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       contract.Timeout,
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(req)
	if err != nil {
		return Verdict{Status: "UNKNOWN", Reasons: []string{"HTTP observation unavailable"}}, nil
	}
	defer response.Body.Close()
	verdict := Verdict{Status: "VERIFIED", ObservedStatus: response.StatusCode}
	if response.StatusCode != contract.ExpectedStatus {
		verdict.Status = "FAILED"
		verdict.Reasons = append(verdict.Reasons, fmt.Sprintf("expected HTTP status %d, observed %d", contract.ExpectedStatus, response.StatusCode))
	}
	if contract.ExpectedBodySubstring != "" {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
		if readErr != nil {
			return Verdict{Status: "UNKNOWN", Reasons: []string{"HTTP response body unreadable"}}, nil
		}
		if len(body) > maxResponseBytes {
			return Verdict{Status: "UNKNOWN", Reasons: []string{"HTTP response body exceeds verification limit"}}, nil
		}
		if !strings.Contains(string(body), contract.ExpectedBodySubstring) {
			verdict.Status = "FAILED"
			verdict.Reasons = append(verdict.Reasons, "response body did not contain expected text")
		}
	}
	return verdict, nil
}
func (v Verdict) JSON() string { b, _ := json.MarshalIndent(v, "", "  "); return string(b) }
