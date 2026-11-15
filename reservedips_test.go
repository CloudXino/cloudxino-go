package cloudxino

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func rip(status string, server *string) map[string]any {
	return map[string]any{"id": "r1", "ip_address": "203.0.113.10", "region": "dhaka-1", "status": status, "server_id": server}
}

func TestReservedIPsListFiltersByAddress(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, []any{rip("available", nil)})
	})
	ips, err := c.ReservedIPs.List(context.Background(), &ListReservedIPsOptions{IPAddress: "203.0.113.10"})
	if err != nil || len(ips) != 1 || *ips[0].IpAddress != "203.0.113.10" {
		t.Fatalf("ips=%v err=%v", ips, err)
	}
	if got := f.calls[0]; got.Method != http.MethodGet || got.Path != "/api/v1/reserved-ips" || got.Query != "ip_address=203.0.113.10" {
		t.Fatalf("sent %s %s?%s", got.Method, got.Path, got.Query)
	}
	if _, err := c.ReservedIPs.List(context.Background(), nil); err != nil || f.calls[1].Query != "" {
		t.Fatalf("query %q err=%v", f.calls[1].Query, err)
	}
}

func TestReservedIPsGet(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, rip("available", nil))
	})
	ip, err := c.ReservedIPs.Get(context.Background(), "r1")
	if err != nil || ip.Id != "r1" || *ip.Status != ReservedIPStatusAvailable {
		t.Fatalf("ip=%v err=%v", ip, err)
	}
	if f.calls[0].Path != "/api/v1/reserved-ips/r1" {
		t.Fatal(f.calls[0].Path)
	}
}

func TestReservedIPsCreate(t *testing.T) {
	srv := "s1"
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 201, rip("assigned", &srv))
	})
	region := "dhaka-1"
	ip, err := c.ReservedIPs.Create(context.Background(), CreateReservedIPRequest{Region: &region, ServerId: &srv}, "my-key")
	if err != nil || *ip.ServerId != "s1" {
		t.Fatalf("ip=%v err=%v", ip, err)
	}
	got := f.calls[0]
	if got.Method != http.MethodPost || got.Path != "/api/v1/reserved-ips" || got.Header.Get("Idempotency-Key") != "my-key" {
		t.Fatalf("sent %s %s key=%q", got.Method, got.Path, got.Header.Get("Idempotency-Key"))
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(got.Body), &body)
	if body["region"] != "dhaka-1" || body["server_id"] != "s1" {
		t.Fatalf("body %s", got.Body)
	}
	if _, ok := body["project_id"]; ok {
		t.Fatalf("empty project_id sent: %s", got.Body)
	}
}

func TestReservedIPsAssignUnassignDelete(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 1:
			srv := "s2"
			writeJSON(w, 200, rip("assigned", &srv))
		case 2:
			writeJSON(w, 200, rip("available", nil))
		default:
			w.WriteHeader(204)
		}
	})
	ctx := context.Background()
	ip, err := c.ReservedIPs.Assign(ctx, "r1", "s2")
	if err != nil || *ip.ServerId != "s2" {
		t.Fatalf("assign ip=%v err=%v", ip, err)
	}
	if got := f.calls[0]; got.Method != http.MethodPost || got.Path != "/api/v1/reserved-ips/r1/assign" || got.Body != `{"server_id":"s2"}` {
		t.Fatalf("assign sent %s %s %s", got.Method, got.Path, got.Body)
	}
	if ip, err = c.ReservedIPs.Unassign(ctx, "r1"); err != nil || ip.ServerId != nil {
		t.Fatalf("unassign ip=%v err=%v", ip, err)
	}
	if got := f.calls[1]; got.Method != http.MethodPost || got.Path != "/api/v1/reserved-ips/r1/unassign" {
		t.Fatalf("unassign sent %s %s", got.Method, got.Path)
	}
	if err := c.ReservedIPs.Delete(ctx, "r1"); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[2]; got.Method != http.MethodDelete || got.Path != "/api/v1/reserved-ips/r1" {
		t.Fatalf("delete sent %s %s", got.Method, got.Path)
	}
}

func TestReservedIPsUnavailableIsNotRetried(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 503, map[string]any{"code": "unavailable", "detail": "Reserved IPs are not available right now"})
	})
	_, err := c.ReservedIPs.Create(context.Background(), CreateReservedIPRequest{})
	if !IsUnavailable(err) {
		t.Fatalf("want unavailable, got %v", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("retried %d times", len(f.calls)-1)
	}
}

func TestMaintenance503StillRetried(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			writeJSON(w, 503, map[string]any{"code": "maintenance", "detail": "back soon"})
			return
		}
		writeJSON(w, 200, []any{})
	})
	if _, err := c.ReservedIPs.List(context.Background(), nil); err != nil || len(f.calls) != 2 {
		t.Fatalf("calls=%d err=%v", len(f.calls), err)
	}
}
