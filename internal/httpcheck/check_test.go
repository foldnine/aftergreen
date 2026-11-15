package httpcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestVerify(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("version=abc123"))
	}))
	defer srv.Close()
	tests := []struct {
		status         int
		contains, want string
	}{
		{200, "version=abc123", "VERIFIED"},
		{201, "version=abc123", "FAILED"},
		{200, "version=wrong", "FAILED"},
	}
	for _, tt := range tests {
		got, err := Verify(context.Background(), Contract{URL: srv.URL, ExpectedStatus: tt.status, ExpectedBodySubstring: tt.contains})
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != tt.want {
			t.Fatalf("got %+v want %s", got, tt.want)
		}
	}
}
func TestUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := Verify(ctx, Contract{URL: "http://127.0.0.1:55555", ExpectedStatus: 200})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "UNKNOWN" {
		t.Fatalf("%+v", got)
	}
}
func TestRejectInvalidURL(t *testing.T) {
	for _, url := range []string{"file:///etc/passwd", "https://user:pass@example.com", "http://", "https://example.com/#secret"} {
		_, err := Verify(context.Background(), Contract{URL: url, ExpectedStatus: 200})
		if err == nil {
			t.Fatalf("accepted URL %q", url)
		}
	}
}
func TestDoNotFollowRedirect(t *testing.T) {
	reached := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true; w.WriteHeader(200) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer redirect.Close()
	got, err := Verify(context.Background(), Contract{URL: redirect.URL, ExpectedStatus: 200, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if reached {
		t.Fatal("redirect target unexpectedly contacted")
	}
	if got.Status != "FAILED" || got.ObservedStatus != 302 {
		t.Fatalf("%+v", got)
	}
}
func TestResponseSizeLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		chunk := make([]byte, maxResponseBytes+1)
		w.Write(chunk)
	}))
	defer srv.Close()
	got, err := Verify(context.Background(), Contract{URL: srv.URL, ExpectedStatus: 200, ExpectedBodySubstring: "healthy"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "UNKNOWN" {
		t.Fatalf("%+v", got)
	}
}
