package cloudxino

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type call struct {
	Method, Path, Query, Body string
	Header                    http.Header
}

// fake is a tiny API: handler answers by "METHOD /path"; every call is recorded.
type fake struct {
	mu    sync.Mutex
	calls []call
	srv   *httptest.Server
}

func newFake(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, n int)) (*Client, *fake) {
	t.Helper()
	f := &fake{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.calls = append(f.calls, call{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, string(body), r.Header.Clone()})
		n := len(f.calls)
		f.mu.Unlock()
		handler(w, r, n)
	}))
	t.Cleanup(f.srv.Close)
	c, err := NewClient("cxk_test", WithBaseURL(f.srv.URL+"/api"), WithRetries(3, time.Millisecond, 5*time.Millisecond), WithUserAgent("test/1"))
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestNewClientNeedsToken(t *testing.T) {
	if _, err := NewClient("  "); err == nil {
		t.Fatal("want an error without a token")
	}
	if _, err := NewClient("cxk_x", WithBaseURL("not a url")); err == nil {
		t.Fatal("want an error for a bad base URL")
	}
}

func TestHeadersAndPath(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, []map[string]any{{"id": "k1", "name": "laptop", "public_key": "ssh-ed25519 AAAA", "fingerprint": "SHA256:x"}})
	})
	keys, err := c.SSHKeys.List(context.Background(), &ListSSHKeysOptions{Name: "laptop"})
	if err != nil || len(keys) != 1 || keys[0].Id != "k1" {
		t.Fatalf("keys=%v err=%v", keys, err)
	}
	got := f.calls[0]
	if got.Path != "/api/v1/ssh-keys" || got.Query != "name=laptop" {
		t.Fatalf("path %s?%s", got.Path, got.Query)
	}
	if got.Header.Get("Authorization") != "Bearer cxk_test" {
		t.Fatal("no bearer token")
	}
	if ua := got.Header.Get("User-Agent"); !strings.HasPrefix(ua, "test/1 cloudxino-go/") {
		t.Fatalf("user agent %q", ua)
	}
	if got.Header.Get("Idempotency-Key") != "" {
		t.Fatal("GET must not carry an Idempotency-Key")
	}
}

func TestPathSegmentsAreEscaped(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) { writeJSON(w, 200, map[string]any{"id": "x"}) })
	_, _ = c.Servers.Get(context.Background(), "../api/keys")
	if got := f.calls[0].Path; got != "/api/v1/mc2/..%2Fapi%2Fkeys" {
		t.Fatalf("id not escaped as one segment: %s", got)
	}
}

func TestRetriesRateLimitWithSameIdempotencyKey(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n < 3 {
			w.Header().Set("Retry-After", "0")
			writeJSON(w, 429, map[string]any{"detail": "Too many requests", "code": "rate_limited"})
			return
		}
		writeJSON(w, 201, map[string]any{"id": "s1", "name": "web-01", "status": "creating"})
	})
	srv, err := c.Servers.Create(context.Background(), CreateMC2Request{Name: "web-01", Image: "debian-13", Size: "s-1vcpu-1gb"})
	if err != nil || srv.Id != "s1" {
		t.Fatalf("srv=%v err=%v", srv, err)
	}
	if len(f.calls) != 3 {
		t.Fatalf("%d calls, want 3", len(f.calls))
	}
	key := f.calls[0].Header.Get("Idempotency-Key")
	if key == "" {
		t.Fatal("POST without Idempotency-Key")
	}
	for _, cl := range f.calls {
		if cl.Header.Get("Idempotency-Key") != key {
			t.Fatal("retries must reuse the same Idempotency-Key")
		}
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(f.calls[0].Body), &body)
	if _, sent := body["password"]; sent {
		t.Fatal("unset optional fields must be left out, not sent as null")
	}
}

func TestOwnIdempotencyKey(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) { writeJSON(w, 201, map[string]any{"id": "fw"}) })
	if _, err := c.Firewalls.Create(context.Background(), CreateFirewallRequest{Name: "web"}, "my-key"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].Header.Get("Idempotency-Key") != "my-key" {
		t.Fatal("own key not sent")
	}
}

func TestErrorsAreNotRetriedAndParsed(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 404, map[string]any{"detail": "Firewall not found", "code": "not_found"})
	})
	_, err := c.Firewalls.Get(context.Background(), "nope")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != CodeNotFound || apiErr.Message != "Firewall not found" || !IsNotFound(err) {
		t.Fatalf("err=%v", err)
	}
	if len(f.calls) != 1 {
		t.Fatal("a 404 must not be retried")
	}
}

func TestValidationErrorMessage(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 422, map[string]any{"code": "validation_error", "detail": []map[string]any{
			{"loc": []any{"body", "name"}, "msg": "Field required"},
			{"loc": []any{"body", "size"}, "msg": "Field required"},
		}})
	})
	_, err := c.Servers.Create(context.Background(), CreateMC2Request{})
	if ErrorCode(err) != CodeValidation || !strings.Contains(err.Error(), "name: Field required; size: Field required") {
		t.Fatalf("err=%v", err)
	}
}

func TestOldErrorBodyWithoutCode(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 402, map[string]any{"detail": "Insufficient BDT balance"})
	})
	_, err := c.Servers.Create(context.Background(), CreateMC2Request{Name: "a", Image: "b", Size: "c"})
	if ErrorCode(err) != CodeInsufficientBalance {
		t.Fatalf("err=%v", err)
	}
}

func TestGivesUpAfterRetries(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 503, map[string]any{"detail": "Down for maintenance", "code": "maintenance"})
	})
	_, err := c.Projects.List(context.Background())
	if ErrorCode(err) != CodeMaintenance || len(f.calls) != 4 {
		t.Fatalf("err=%v calls=%d", err, len(f.calls))
	}
}

func TestContextCancelStopsRetrying(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Retry-After", "60")
		writeJSON(w, 429, map[string]any{"detail": "slow down", "code": "rate_limited"})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Projects.List(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 2*time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
}

func TestWaitReadyAndDeleted(t *testing.T) {
	status := []string{"creating", "creating", "running"}
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n <= len(status) {
			writeJSON(w, 200, map[string]any{"id": "s1", "status": status[n-1]})
			return
		}
		writeJSON(w, 404, map[string]any{"detail": "CC2 instance not found", "code": "not_found"})
	})
	opts := &WaitOptions{Interval: time.Millisecond}
	srv, err := c.Servers.WaitReady(context.Background(), "s1", opts)
	if err != nil || strv(srv.Status) != ServerRunning {
		t.Fatalf("srv=%v err=%v", srv, err)
	}
	if err := c.Servers.WaitDeleted(context.Background(), "s1", opts); err != nil {
		t.Fatal(err)
	}
}

func TestWaitReadyFailsOnError(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, map[string]any{"id": "s1", "status": "error"})
	})
	_, err := c.Servers.WaitReady(context.Background(), "s1", &WaitOptions{Interval: time.Millisecond})
	var stuck *ErrStuck
	if !errors.As(err, &stuck) || stuck.Status != "error" {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitResizedReportsFailure(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, map[string]any{"id": "s1", "status": "running", "size": "s-1vcpu-1gb",
			"resize": map[string]any{"to": "s-2vcpu-2gb", "error": "Not enough room on the host"}})
	})
	_, err := c.Servers.WaitResized(context.Background(), "s1", "s-2vcpu-2gb", &WaitOptions{Interval: time.Millisecond})
	if err == nil || !strings.Contains(err.Error(), "Not enough room") {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitTimeout(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, map[string]any{"id": "s1", "status": "creating"})
	})
	_, err := c.Servers.WaitReady(context.Background(), "s1", &WaitOptions{Interval: time.Millisecond, Timeout: 20 * time.Millisecond})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestDNSRecordCreateReturnsTheRecord(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 201, map[string]any{
			"id": "z1", "name": "example.com",
			"records": []map[string]any{{"id": "r0"}, {"id": "r1"}},
			"record":  map[string]any{"id": "r1", "name": "www", "type": "A", "content": "203.0.113.10", "ttl": 300},
		})
	})
	ttl := 300
	name := "www"
	rec, err := c.DNS.CreateRecord(context.Background(), "z1", RecordIn{Name: &name, Type: "A", Content: "203.0.113.10", Ttl: &ttl})
	if err != nil || rec.Id != "r1" || *rec.Ttl != 300 {
		t.Fatalf("rec=%v err=%v", rec, err)
	}
	if f.calls[0].Path != "/api/v1/dns/zones/z1/records" {
		t.Fatal(f.calls[0].Path)
	}
}

func TestRawAnswers(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("$ORIGIN example.com.\n"))
	})
	text, err := c.DNS.Export(context.Background(), "z1")
	if err != nil || !strings.HasPrefix(text, "$ORIGIN") {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestDeleteNoContent(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) { w.WriteHeader(204) })
	if err := c.SSHKeys.Delete(context.Background(), "k1"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0].Method != http.MethodDelete || f.calls[0].Path != "/api/v1/ssh-keys/k1" {
		t.Fatal(f.calls[0])
	}
}

func TestRetryAfterDate(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", time.Now().Add(3*time.Second).UTC().Format(http.TimeFormat))
	if d := retryAfter(h); d < time.Second || d > 4*time.Second {
		t.Fatalf("got %s", d)
	}
}
