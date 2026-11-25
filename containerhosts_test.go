package cloudxino

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func host(status string) map[string]any {
	return map[string]any{"id": "h1", "name": "apps", "plan": "s-2vcpu-4gb", "status": status, "error": nil,
		"project_id": "p1", "instance_id": "i1",
		"server":        map[string]any{"status": "running", "ip_address": "203.0.113.30", "region": "dhaka-1", "cpu": 2, "memory": 4096, "disk": 80},
		"portainer_url": "https://203-0-113-30.sslip.io", "domains": []any{},
		"price":      map[string]any{"server_per_month": 1799, "fee_per_month": 400, "per_month": 2199},
		"created_at": "2026-11-20T10:00:00+00:00", "ready_at": nil, "last_deployment": nil}
}

func deployment(status string) map[string]any {
	return map[string]any{"id": "d1", "number": 3, "status": status, "note": nil, "env_keys": []string{"DB_URL"},
		"domains":    []any{map[string]any{"domain": "app.example.com", "service": "web", "port": 8080}},
		"created_by": "dev@example.com", "started_at": "2026-11-20T10:05:00+00:00", "finished_at": nil,
		"compose": "services:\n  web:\n    image: nginx\n", "output": "line 1\nline 2\n"}
}

func TestContainerHostsOptions(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, map[string]any{"enabled": true, "plans": []any{map[string]any{"slug": "s-1vcpu-2gb", "cpu": 1, "memory_gb": 2.0,
			"disk_gb": 50, "server_per_month": 999, "fee_per_month": 300, "per_month": 1299, "per_hour": 1.8042}}})
	})
	o, err := c.ContainerHosts.Options(context.Background())
	if err != nil || !o.Enabled || len(o.Plans) != 1 || o.Plans[0].Slug != "s-1vcpu-2gb" || o.Plans[0].PerMonth != 1299 || o.Plans[0].MemoryGb != 2 {
		t.Fatalf("o=%+v err=%v", o, err)
	}
	if f.calls[0].Path != "/api/v1/container-hosts/options" {
		t.Fatal(f.calls[0].Path)
	}
}

func TestContainerHostsCreate(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 201, host("creating"))
	})
	h, err := c.ContainerHosts.Create(context.Background(), CreateContainerHostRequest{Name: "apps", Plan: "s-2vcpu-4gb"}, "my-key")
	if err != nil || h.Id != "h1" || h.Status != ContainerHostCreating || h.Server.IpAddress != "203.0.113.30" || h.Price.PerMonth != 2199 {
		t.Fatalf("h=%+v err=%v", h, err)
	}
	got := f.calls[0]
	if got.Method != http.MethodPost || got.Path != "/api/v1/container-hosts" || got.Header.Get("Idempotency-Key") != "my-key" {
		t.Fatalf("sent %s %s key=%q", got.Method, got.Path, got.Header.Get("Idempotency-Key"))
	}
	if got.Body != `{"name":"apps","plan":"s-2vcpu-4gb"}` {
		t.Fatalf("body %s", got.Body)
	}
}

func TestContainerHostsListGetDeleteRetry(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 1:
			writeJSON(w, 200, []any{host("ready")})
		case 2:
			h := host("ready")
			h["compose"], h["env_keys"] = "services: {}", []string{"A"}
			h["firewalls"] = []any{map[string]any{"id": "fw1", "name": "web"}}
			h["last_deployment"] = deployment("succeeded")
			writeJSON(w, 200, h)
		case 3:
			writeJSON(w, 200, map[string]any{"message": "Deleting the host and its server"})
		default:
			writeJSON(w, 200, host("creating"))
		}
	})
	ctx := context.Background()
	list, err := c.ContainerHosts.List(ctx, &ListContainerHostsOptions{ProjectID: "p1"})
	if err != nil || len(list) != 1 || list[0].Name != "apps" {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if got := f.calls[0]; got.Path != "/api/v1/container-hosts" || got.Query != "project_id=p1" {
		t.Fatalf("list sent %s?%s", got.Path, got.Query)
	}
	h, err := c.ContainerHosts.Get(ctx, "h1")
	if err != nil || *h.Compose != "services: {}" || h.EnvKeys[0] != "A" || h.Firewalls[0].Name != "web" || h.LastDeployment.Number != 3 {
		t.Fatalf("h=%+v err=%v", h, err)
	}
	if err := c.ContainerHosts.Delete(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[2]; got.Method != http.MethodDelete || got.Path != "/api/v1/container-hosts/h1" {
		t.Fatalf("delete sent %s %s", got.Method, got.Path)
	}
	if _, err := c.ContainerHosts.Retry(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[3]; got.Method != http.MethodPost || got.Path != "/api/v1/container-hosts/h1/retry" {
		t.Fatalf("retry sent %s %s", got.Method, got.Path)
	}
}

func TestContainerHostsCredentialsAndStatus(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if strings.HasSuffix(r.URL.Path, "/credentials") {
			writeJSON(w, 200, map[string]any{"portainer_url": "https://203-0-113-30.sslip.io", "username": "admin", "password": "s3cret", "note": "first password"})
			return
		}
		writeJSON(w, 200, map[string]any{
			"containers": []any{
				map[string]any{"id": "abc", "name": "app-web-1", "image": "nginx", "state": "running", "status": "Up 2 hours", "ports": "80/tcp",
					"service": "web", "system": false, "created": "2026-11-20", "stats": map[string]any{"cpu": "0.15%", "memory": "21MiB / 3.8GiB"}},
				map[string]any{"id": "def", "name": "traefik", "image": "traefik:v3.6", "state": "running", "service": nil, "system": true, "stats": nil},
			},
			"disk":         map[string]any{"total": 80000000, "used": 5000000, "free": 75000000},
			"certificates": []string{"app.example.com"},
			"domains": []any{map[string]any{"domain": "app.example.com", "service": "web", "port": 8080, "dns": []string{"203.0.113.30"},
				"points_here": true, "ssl": "active"}},
		})
	})
	ctx := context.Background()
	cr, err := c.ContainerHosts.Credentials(ctx, "h1")
	if err != nil || cr.Username != "admin" || cr.Password != "s3cret" {
		t.Fatalf("cr=%+v err=%v", cr, err)
	}
	st, err := c.ContainerHosts.Status(ctx, "h1")
	if err != nil || len(st.Containers) != 2 || st.Containers[0].Stats.Cpu != "0.15%" || !st.Containers[1].System || st.Containers[1].Stats != nil {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if st.Disk.Free != 75000000 || st.Domains[0].SSL != DomainSSLActive || !st.Domains[0].PointsHere || st.Domains[0].Port != 8080 {
		t.Fatalf("st=%+v", st)
	}
	if f.calls[0].Path != "/api/v1/container-hosts/h1/credentials" || f.calls[1].Path != "/api/v1/container-hosts/h1/status" {
		t.Fatal(f.calls[0].Path, f.calls[1].Path)
	}
}

func TestContainerHostsLogsAndActions(t *testing.T) {
	const logs = "2026-11-20T10:00:00Z hello\n"
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(logs))
			return
		}
		writeJSON(w, 200, map[string]any{"message": "restart app-web-1: done"})
	})
	ctx := context.Background()
	got, err := c.ContainerHosts.Logs(ctx, "h1", "app-web-1", 50)
	if err != nil || got != logs {
		t.Fatalf("got %q err=%v", got, err)
	}
	if call := f.calls[0]; call.Path != "/api/v1/container-hosts/h1/containers/app-web-1/logs" || call.Query != "lines=50" {
		t.Fatalf("logs sent %s?%s", call.Path, call.Query)
	}
	if _, err := c.ContainerHosts.Logs(ctx, "h1", "app-web-1", 0); err != nil || f.calls[1].Query != "" {
		t.Fatalf("query %q err=%v", f.calls[1].Query, err)
	}
	if err := c.ContainerHosts.ContainerAction(ctx, "h1", "app-web-1", ContainerRestart); err != nil {
		t.Fatal(err)
	}
	if call := f.calls[2]; call.Method != http.MethodPost || call.Path != "/api/v1/container-hosts/h1/containers/app-web-1/action" || call.Body != `{"action":"restart"}` {
		t.Fatalf("action sent %s %s %s", call.Method, call.Path, call.Body)
	}
}

func TestContainerHostsStopSystemContainerRefused(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 409, map[string]any{"code": "conflict", "detail": "This container runs the reverse proxy or Portainer"})
	})
	err := c.ContainerHosts.ContainerAction(context.Background(), "h1", "traefik", ContainerStop)
	if !IsConflict(err) {
		t.Fatalf("err=%v", err)
	}
}

func TestContainerHostsValidateAndDeploy(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if strings.HasSuffix(r.URL.Path, "/validate") {
			writeJSON(w, 200, map[string]any{"services": []string{"web", "db"}})
			return
		}
		writeJSON(w, 202, deployment("running"))
	})
	ctx := context.Background()
	compose := "services:\n  web:\n    image: nginx\n"
	svcs, err := c.ContainerHosts.Validate(ctx, "h1", compose)
	if err != nil || strings.Join(svcs, ",") != "web,db" {
		t.Fatalf("svcs=%v err=%v", svcs, err)
	}
	note := "from CI"
	d, err := c.ContainerHosts.Deploy(ctx, "h1", DeployRequest{Compose: &compose, Note: &note})
	if err != nil || d.Status != DeploymentRunning || d.Number != 3 || *d.Compose != compose || d.Domains[0].Service != "web" {
		t.Fatalf("d=%+v err=%v", d, err)
	}
	got := f.calls[1]
	if got.Method != http.MethodPost || got.Path != "/api/v1/container-hosts/h1/deploy" || got.Header.Get("Idempotency-Key") == "" {
		t.Fatalf("deploy sent %s %s", got.Method, got.Path)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(got.Body), &body)
	if body["compose"] != compose || body["note"] != "from CI" {
		t.Fatalf("body %s", got.Body)
	}
	// the saved compose file again: nothing but {}
	if _, err := c.ContainerHosts.Deploy(ctx, "h1", DeployRequest{}); err != nil || f.calls[2].Body != `{}` {
		t.Fatalf("body %s err=%v", f.calls[2].Body, err)
	}
}

func TestContainerHostsEnv(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 1:
			writeJSON(w, 200, map[string]any{"env": map[string]string{"A": "1"}})
		case 2:
			writeJSON(w, 200, map[string]any{"env_keys": []string{"A", "B"}, "deployment": deployment("running")})
		case 3:
			writeJSON(w, 200, map[string]any{"env_keys": []string{}, "deployment": nil})
		case 4:
			writeJSON(w, 200, map[string]any{"env": map[string]string{}})
		default:
			writeJSON(w, 200, map[string]any{"env_keys": []string{"X"}, "deployment": nil})
		}
	})
	ctx := context.Background()
	env, err := c.ContainerHosts.GetEnv(ctx, "h1")
	if err != nil || env["A"] != "1" {
		t.Fatalf("env=%v err=%v", env, err)
	}
	res, err := c.ContainerHosts.SetEnv(ctx, "h1", map[string]string{"A": "1", "B": "2"}, true)
	if err != nil || len(res.EnvKeys) != 2 || res.Deployment == nil || res.Deployment.Id != "d1" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := f.calls[1]; got.Method != http.MethodPut || got.Path != "/api/v1/container-hosts/h1/env" || got.Body != `{"env":{"A":"1","B":"2"},"deploy":true}` {
		t.Fatalf("put sent %s %s %s", got.Method, got.Path, got.Body)
	}
	// clearing every var sends an empty map, not null
	if res, err = c.ContainerHosts.SetEnv(ctx, "h1", nil, false); err != nil || res.Deployment != nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if got := f.calls[2].Body; got != `{"env":{},"deploy":false}` {
		t.Fatalf("body %s", got)
	}
	if env, err = c.ContainerHosts.GetEnv(ctx, "h1"); err != nil || env == nil || len(env) != 0 {
		t.Fatalf("env=%v err=%v", env, err)
	}
	if _, err := c.ContainerHosts.SetEnvText(ctx, "h1", "X=1\n", false); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[4].Body; got != `{"text":"X=1\n","deploy":false}` {
		t.Fatalf("body %s", got)
	}
}

func TestContainerHostsDomains(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, map[string]any{"domains": []any{map[string]any{"domain": "app.example.com", "service": "web", "port": 8080}}, "deployment": nil})
	})
	res, err := c.ContainerHosts.SetDomains(context.Background(), "h1", []ContainerDomain{{Domain: "app.example.com", Service: "web", Port: 8080}}, false)
	if err != nil || len(res.Domains) != 1 || res.Deployment != nil {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	got := f.calls[0]
	if got.Method != http.MethodPut || got.Path != "/api/v1/container-hosts/h1/domains" ||
		got.Body != `{"domains":[{"domain":"app.example.com","service":"web","port":8080}],"deploy":false}` {
		t.Fatalf("sent %s %s %s", got.Method, got.Path, got.Body)
	}
	if _, err := c.ContainerHosts.SetDomains(context.Background(), "h1", nil, true); err != nil || f.calls[1].Body != `{"domains":[],"deploy":true}` {
		t.Fatalf("body %s err=%v", f.calls[1].Body, err)
	}
}

func TestContainerHostsChangePlanAndWait(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		h := host("ready")
		switch n {
		case 1:
			h["server"].(map[string]any)["status"] = "resizing"
			writeJSON(w, 202, h)
		case 2:
			h["server"].(map[string]any)["status"] = "resizing"
			h["resize"] = map[string]any{"to": "s-4vcpu-8gb", "error": nil}
			writeJSON(w, 200, h)
		default:
			h["plan"] = "s-4vcpu-8gb"
			writeJSON(w, 200, h)
		}
	})
	ctx := context.Background()
	if _, err := c.ContainerHosts.ChangePlan(ctx, "h1", "s-4vcpu-8gb"); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[0]; got.Method != http.MethodPost || got.Path != "/api/v1/container-hosts/h1/plan" || got.Body != `{"plan":"s-4vcpu-8gb"}` {
		t.Fatalf("sent %s %s %s", got.Method, got.Path, got.Body)
	}
	h, err := c.ContainerHosts.WaitPlan(ctx, "h1", "s-4vcpu-8gb", &WaitOptions{Interval: time.Millisecond})
	if err != nil || h.Plan != "s-4vcpu-8gb" || len(f.calls) != 3 {
		t.Fatalf("h=%+v calls=%d err=%v", h, len(f.calls), err)
	}
}

func TestContainerHostsWaitPlanResizeFailed(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		h := host("ready")
		h["resize"] = map[string]any{"to": "s-4vcpu-8gb", "error": "not enough room on the node"}
		writeJSON(w, 200, h)
	})
	_, err := c.ContainerHosts.WaitPlan(context.Background(), "h1", "s-4vcpu-8gb", &WaitOptions{Interval: time.Millisecond})
	var stuck *ErrStuck
	if !errors.As(err, &stuck) || stuck.Detail != "not enough room on the node" {
		t.Fatalf("err=%v", err)
	}
}

func TestContainerHostsDeployments(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch {
		case r.Method == http.MethodPost:
			d := deployment("running")
			d["id"], d["number"], d["note"] = "d2", 4, "Redeploy of #3"
			writeJSON(w, 202, d)
		case strings.HasSuffix(r.URL.Path, "/deployments"):
			d := deployment("succeeded")
			delete(d, "compose")
			delete(d, "output")
			writeJSON(w, 200, []any{d})
		default:
			writeJSON(w, 200, deployment("succeeded"))
		}
	})
	ctx := context.Background()
	list, err := c.ContainerHosts.ListDeployments(ctx, "h1", &ListDeploymentsOptions{Limit: 10})
	if err != nil || len(list) != 1 || list[0].Compose != nil || list[0].EnvKeys[0] != "DB_URL" {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	if got := f.calls[0]; got.Path != "/api/v1/container-hosts/h1/deployments" || got.Query != "limit=10" {
		t.Fatalf("list sent %s?%s", got.Path, got.Query)
	}
	d, err := c.ContainerHosts.GetDeployment(ctx, "h1", "d1")
	if err != nil || *d.Output != "line 1\nline 2\n" || f.calls[1].Path != "/api/v1/container-hosts/h1/deployments/d1" {
		t.Fatalf("d=%+v err=%v path=%s", d, err, f.calls[1].Path)
	}
	d, err = c.ContainerHosts.Redeploy(ctx, "h1", "d1")
	if err != nil || d.Id != "d2" || *d.Note != "Redeploy of #3" {
		t.Fatalf("d=%+v err=%v", d, err)
	}
	if got := f.calls[2]; got.Method != http.MethodPost || got.Path != "/api/v1/container-hosts/h1/deployments/d1/redeploy" {
		t.Fatalf("redeploy sent %s %s", got.Method, got.Path)
	}
}

func TestContainerHostsWaitReady(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 1:
			writeJSON(w, 200, host("creating"))
		case 2:
			writeJSON(w, 200, host("installing"))
		default:
			writeJSON(w, 200, host("ready"))
		}
	})
	h, err := c.ContainerHosts.WaitReady(context.Background(), "h1", &WaitOptions{Interval: time.Millisecond})
	if err != nil || h.Status != ContainerHostReady {
		t.Fatalf("h=%+v err=%v", h, err)
	}
}

func TestContainerHostsWaitReadyFailed(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		h := host("failed")
		h["error"], h["log_tail"] = "The installer failed (exit 1)", "a\nb\nE: could not get lock\n"
		writeJSON(w, 200, h)
	})
	h, err := c.ContainerHosts.WaitReady(context.Background(), "h1", &WaitOptions{Interval: time.Millisecond})
	var stuck *ErrStuck
	if !errors.As(err, &stuck) || stuck.Status != ContainerHostFailed || !strings.Contains(stuck.Detail, "exit 1") ||
		!strings.HasSuffix(stuck.Detail, "E: could not get lock") || h == nil {
		t.Fatalf("err=%v h=%v", err, h)
	}
}

func TestContainerHostsWaitDeleted(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n < 3 {
			writeJSON(w, 200, host("deleting"))
			return
		}
		writeJSON(w, 404, map[string]any{"code": "not_found", "detail": "Container host not found"})
	})
	if err := c.ContainerHosts.WaitDeleted(context.Background(), "h1", &WaitOptions{Interval: time.Millisecond}); err != nil || len(f.calls) != 3 {
		t.Fatalf("calls=%d err=%v", len(f.calls), err)
	}
}

func TestContainerHostsWaitDeployment(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n < 3 {
			writeJSON(w, 200, deployment("running"))
			return
		}
		writeJSON(w, 200, deployment("succeeded"))
	})
	d, err := c.ContainerHosts.WaitDeployment(context.Background(), "h1", "d1", &WaitOptions{Interval: time.Millisecond})
	if err != nil || d.Status != DeploymentSucceeded {
		t.Fatalf("d=%+v err=%v", d, err)
	}
}

func TestContainerHostsWaitDeploymentFailed(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		d := deployment("failed")
		d["output"] = "Pulling web\nError: pull access denied for nope\n"
		writeJSON(w, 200, d)
	})
	d, err := c.ContainerHosts.WaitDeployment(context.Background(), "h1", "d1", &WaitOptions{Interval: time.Millisecond})
	var stuck *ErrStuck
	if !errors.As(err, &stuck) || stuck.ID != "#3" || !strings.Contains(stuck.Detail, "pull access denied") || d == nil || d.Status != DeploymentFailed {
		t.Fatalf("err=%v d=%v", err, d)
	}
}

func TestContainerHostsNotReady(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 409, map[string]any{"code": "conflict", "detail": "The host is not ready yet"})
	})
	_, err := c.ContainerHosts.Deploy(context.Background(), "h1", DeployRequest{})
	if !IsConflict(err) || !strings.Contains(err.Error(), "not ready") {
		t.Fatalf("err=%v", err)
	}
}

func TestTail(t *testing.T) {
	if got := tail("a\nb\nc\n", 2); got != "b\nc" {
		t.Fatalf("%q", got)
	}
	if got := tail("", 3); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestContainerHostsNat(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/options"):
			writeJSON(w, 200, map[string]any{"enabled": true, "ip_types": []string{"public", "nat"},
				"nat":   map[string]any{"web_ip": "103.143.148.30", "web_host": "nat.cloudxino.com", "max_domains": 9},
				"plans": []any{map[string]any{"slug": "s-2vcpu-4gb", "per_month": 2199, "nat_server_per_month": 1399, "nat_per_month": 1799}}})
		case strings.HasSuffix(r.URL.Path, "/status"):
			writeJSON(w, 200, map[string]any{"containers": []any{}, "certificates": []string{},
				"domains": []any{map[string]any{"domain": "app.example.com", "service": "web", "port": 80, "dns": []string{"103.143.148.30"},
					"points_here": true, "ssl": "waiting_dns", "route": "pending", "route_error": "DNS lookup failed",
					"txt_name": "_cloudxino.app.example.com", "txt_value": "cx-abc"}}})
		default:
			h := host("creating")
			h["ip_type"], h["ingress_ip"] = "nat", "103.143.148.30"
			h["server"].(map[string]any)["ip_address"] = "172.16.0.21"
			h["nat"] = map[string]any{"web_ip": "103.143.148.30", "web_host": "nat.cloudxino.com", "private_ip": "172.16.0.21", "max_domains": 9, "ports": []int{80, 443}}
			writeJSON(w, 201, h)
		}
	})
	ctx := context.Background()
	o, err := c.ContainerHosts.Options(ctx)
	if err != nil || len(o.IpTypes) != 2 || o.Nat.WebIp != "103.143.148.30" || o.Nat.MaxDomains != 9 || o.Plans[0].NatPerMonth != 1799 {
		t.Fatalf("o=%+v err=%v", o, err)
	}
	h, err := c.ContainerHosts.Create(ctx, CreateContainerHostRequest{Name: "apps", Plan: "s-2vcpu-4gb", IpType: ContainerHostIpNat}, "")
	if err != nil || h.IpType != ContainerHostIpNat || h.IngressIp == nil || *h.IngressIp != "103.143.148.30" || *h.Nat.PrivateIp != "172.16.0.21" {
		t.Fatalf("h=%+v err=%v", h, err)
	}
	if f.calls[1].Body != `{"name":"apps","plan":"s-2vcpu-4gb","ip_type":"nat"}` {
		t.Fatalf("body %s", f.calls[1].Body)
	}
	st, err := c.ContainerHosts.Status(ctx, "h1")
	if err != nil || *st.Domains[0].Route != "pending" || *st.Domains[0].TxtValue != "cx-abc" || !st.Domains[0].PointsHere {
		t.Fatalf("st=%+v err=%v", st, err)
	}
}
