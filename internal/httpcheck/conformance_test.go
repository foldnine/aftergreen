package httpcheck

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// conformanceCase mirrors conformance/http/cases.json (schema_version 1).
type conformanceCase struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Server *struct {
		Status      int    `json:"status"`
		Body        string `json:"body"`
		BodyBytes   *int   `json:"body_bytes"`
		BodySuffix  string `json:"body_suffix"`
		Location    string `json:"location"`
		Gzip        bool   `json:"gzip"`
		Truncate    bool   `json:"truncate"`
		DelayMS     int    `json:"delay_ms"`
		BodyDelayMS int    `json:"body_delay_ms"`
	} `json:"server"`
	Contract struct {
		ExpectedStatus int    `json:"expected_status"`
		Contains       string `json:"contains"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	} `json:"contract"`
	Want struct {
		Verdict                 string           `json:"verdict"`
		ObservedStatus          *json.RawMessage `json:"observed_status"`
		RedirectTargetRequested *bool            `json:"redirect_target_requested"`
	} `json:"want"`
}

func loadConformance(t *testing.T) []conformanceCase {
	t.Helper()
	raw, err := os.ReadFile("../../conformance/http/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		SchemaVersion int               `json:"schema_version"`
		Description   string            `json:"description"`
		Cases         []conformanceCase `json:"cases"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		t.Fatal(err)
	}
	if file.SchemaVersion != 1 || len(file.Cases) == 0 {
		t.Fatalf("unexpected conformance file: version %d, %d cases", file.SchemaVersion, len(file.Cases))
	}
	return file.Cases
}

func unreachableBase(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr
}

func TestConformance(t *testing.T) {
	for _, tc := range loadConformance(t) {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			var mu sync.Mutex
			var requested []string
			base := ""
			if s := tc.Server; s != nil {
				body := []byte(s.Body)
				if s.BodyBytes != nil {
					body = append(bytes.Repeat([]byte("X"), *s.BodyBytes-len(s.BodySuffix)), s.BodySuffix...)
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					requested = append(requested, r.URL.Path)
					mu.Unlock()
					payload := body
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					if s.Location != "" {
						w.Header().Set("Location", s.Location)
					}
					if s.Gzip && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
						var buf bytes.Buffer
						zw := gzip.NewWriter(&buf)
						zw.Write(payload)
						zw.Close()
						payload = buf.Bytes()
						w.Header().Set("Content-Encoding", "gzip")
					}
					if s.DelayMS > 0 {
						select {
						case <-time.After(time.Duration(s.DelayMS) * time.Millisecond):
						case <-r.Context().Done():
							return
						}
					}
					length := len(payload)
					if s.Truncate {
						length += 100
					}
					w.Header().Set("Content-Length", itoa(length))
					w.WriteHeader(s.Status)
					if s.BodyDelayMS > 0 {
						w.(http.Flusher).Flush()
						select {
						case <-time.After(time.Duration(s.BodyDelayMS) * time.Millisecond):
						case <-r.Context().Done():
							return
						}
					}
					w.Write(payload)
					if s.Truncate {
						w.(http.Flusher).Flush()
						conn, _, err := w.(http.Hijacker).Hijack()
						if err == nil {
							conn.Close()
						}
					}
				}))
				defer srv.Close()
				base = srv.URL
			}
			url := tc.URL
			if url == "" {
				url = "{base}/check"
			}
			url = strings.ReplaceAll(url, "{base}", base)
			if strings.Contains(url, "{unreachable}") {
				url = strings.ReplaceAll(url, "{unreachable}", unreachableBase(t))
			}

			got, err := Verify(context.Background(), Contract{
				URL:                   url,
				ExpectedStatus:        tc.Contract.ExpectedStatus,
				ExpectedBodySubstring: tc.Contract.Contains,
				Timeout:               time.Duration(tc.Contract.TimeoutSeconds) * time.Second,
			})
			if tc.Want.Verdict == "INVALID" {
				if err == nil {
					t.Fatalf("invalid contract accepted: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("contract rejected: %v", err)
			}
			if got.Status != tc.Want.Verdict {
				t.Fatalf("verdict %+v, want %s", got, tc.Want.Verdict)
			}
			if tc.Want.ObservedStatus != nil {
				var want *int
				json.Unmarshal(*tc.Want.ObservedStatus, &want)
				if (want == nil && got.ObservedStatus != 0) || (want != nil && got.ObservedStatus != *want) {
					t.Fatalf("observed status %d, want %s", got.ObservedStatus, *tc.Want.ObservedStatus)
				}
			}
			if got.Status != StatusVerified && len(got.Reasons) == 0 {
				t.Fatal("non-verified result needs a reason")
			}
			if tc.Want.RedirectTargetRequested != nil && !*tc.Want.RedirectTargetRequested {
				mu.Lock()
				defer mu.Unlock()
				if len(requested) != 1 || requested[0] != "/check" {
					t.Fatalf("redirect target contacted: %v", requested)
				}
			}
		})
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
