package httpcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestErrorsNeverEchoURL(t *testing.T) {
	for _, u := range []string{"https://user:tok3n@example.invalid/", "ftp://tok3n.example.invalid/", "https://example.invalid/?k=tok3n#x", "tok3n"} {
		_, err := Verify(context.Background(), Contract{URL: u, ExpectedStatus: 200, Timeout: time.Second})
		if err == nil {
			t.Fatalf("accepted %q", u)
		}
		if strings.Contains(err.Error(), "tok3n") {
			t.Fatalf("error echoes URL: %v", err)
		}
	}
}

func TestRequestShape(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r }))
	defer srv.Close()
	if _, err := Verify(context.Background(), Contract{URL: srv.URL + "/v?x=1", ExpectedStatus: 200, Timeout: time.Second}); err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodGet || got.Header.Get("User-Agent") != UserAgent || got.Header.Get("Authorization") != "" || got.URL.RawQuery != "x=1" {
		t.Fatalf("unexpected request %s %v %v", got.Method, got.URL, got.Header)
	}
}

func TestCallerCancellationIsUnknown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := Verify(ctx, Contract{URL: "http://127.0.0.1:1/", ExpectedStatus: 200, Timeout: time.Second})
	if err != nil || got.Status != StatusUnknown {
		t.Fatalf("%+v %v", got, err)
	}
}
