package cloudxino

import (
	"context"
	"net/http"
	"testing"
)

func TestRegistriesList(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, []any{map[string]any{"id": "r1", "name": "ai-platform", "region": "dhaka-1",
			"endpoint": "registry.example.com/ai-platform-x", "status": "active", "project_id": "p1",
			"created_at": "2026-11-25T10:00:00+00:00", "harbor_robot_token": "secret", "harbor_robot_name": "robot$x"}})
	})
	regs, err := c.Registries.List(context.Background(), &ListRegistriesOptions{ProjectID: "p1"})
	if err != nil || len(regs) != 1 || regs[0].Name != "ai-platform" || regs[0].Status != RegistryActive ||
		regs[0].Endpoint != "registry.example.com/ai-platform-x" {
		t.Fatalf("regs=%+v err=%v", regs, err)
	}
	if got := f.calls[0]; got.Path != "/api/v1/registries" || got.Query != "project_id=p1" {
		t.Fatalf("sent %s ?%s", got.Path, got.Query)
	}
}

func TestContainerHostsRegistries(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 200, map[string]any{"registry_ids": []string{"r1"}, "deployment": deployment("running")})
	})
	res, err := c.ContainerHosts.SetRegistries(context.Background(), "h1", []string{"r1"}, true)
	if err != nil || len(res.RegistryIds) != 1 || res.Deployment == nil || res.Deployment.Status != DeploymentRunning {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	got := f.calls[0]
	if got.Method != http.MethodPut || got.Path != "/api/v1/container-hosts/h1/registries" ||
		got.Body != `{"registry_ids":["r1"],"deploy":true}` {
		t.Fatalf("sent %s %s %s", got.Method, got.Path, got.Body)
	}
	if _, err := c.ContainerHosts.SetRegistries(context.Background(), "h1", nil, false); err != nil ||
		f.calls[1].Body != `{"registry_ids":[],"deploy":false}` {
		t.Fatalf("body %s err=%v", f.calls[1].Body, err)
	}
}
