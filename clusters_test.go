package cloudxino

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func cluster(status string) map[string]any {
	return map[string]any{"id": "c1", "name": "prod", "kubernetes_version": "1.36.5", "region": "dhaka-1",
		"status": status, "project_id": "p1", "organization_id": "o1", "error_message": nil,
		"api_endpoint": "https://k8s.example.com:6401", "ingress": true, "tags": []string{}}
}

func TestClustersCreate(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 201, cluster("pending"))
	})
	count := 3
	cl, err := c.Clusters.Create(context.Background(), CreateClusterRequest{Name: "prod", NodeSize: "k8s-2vcpu-4gb", NodeCount: &count}, "my-key")
	if err != nil || cl.Id != "c1" || cl.Status != ClusterPending {
		t.Fatalf("cl=%v err=%v", cl, err)
	}
	got := f.calls[0]
	if got.Method != http.MethodPost || got.Path != "/api/v1/clusters" || got.Header.Get("Idempotency-Key") != "my-key" {
		t.Fatalf("sent %s %s key=%q", got.Method, got.Path, got.Header.Get("Idempotency-Key"))
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(got.Body), &body)
	if body["name"] != "prod" || body["node_size"] != "k8s-2vcpu-4gb" || body["node_count"] != float64(3) {
		t.Fatalf("body %s", got.Body)
	}
	for _, k := range []string{"region", "kubernetes_version", "project_id", "ingress", "vpc_id"} {
		if _, ok := body[k]; ok {
			t.Fatalf("empty %s sent: %s", k, got.Body)
		}
	}
}

func TestClustersCreateMakesIdempotencyKey(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n == 1 {
			writeJSON(w, 502, map[string]any{"code": "bad_gateway"})
			return
		}
		writeJSON(w, 201, cluster("pending"))
	})
	if _, err := c.Clusters.Create(context.Background(), CreateClusterRequest{Name: "prod", NodeSize: "k8s-2vcpu-4gb"}); err != nil {
		t.Fatal(err)
	}
	k1, k2 := f.calls[0].Header.Get("Idempotency-Key"), f.calls[1].Header.Get("Idempotency-Key")
	if k1 == "" || k1 != k2 {
		t.Fatalf("keys %q %q", k1, k2)
	}
}

func TestClustersListGetDelete(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 1:
			cl := cluster("active")
			cl["node_pools"] = []any{map[string]any{"id": "np1", "name": "default", "count": 2, "status": "active"}}
			writeJSON(w, 200, []any{cl})
		case 2:
			writeJSON(w, 200, cluster("active"))
		default:
			writeJSON(w, 200, map[string]any{"message": "Cluster deletion started"})
		}
	})
	ctx := context.Background()
	list, err := c.Clusters.List(ctx, &ListClustersOptions{ProjectID: "p1"})
	if err != nil || len(list) != 1 || list[0].NodePools[0].Count != 2 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if got := f.calls[0]; got.Path != "/api/v1/clusters" || got.Query != "project_id=p1" {
		t.Fatalf("list sent %s?%s", got.Path, got.Query)
	}
	cl, err := c.Clusters.Get(ctx, "c1")
	if err != nil || *cl.ApiEndpoint != "https://k8s.example.com:6401" || !*cl.Ingress {
		t.Fatalf("cl=%v err=%v", cl, err)
	}
	if f.calls[1].Path != "/api/v1/clusters/c1" {
		t.Fatal(f.calls[1].Path)
	}
	if err := c.Clusters.Delete(ctx, "c1"); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[2]; got.Method != http.MethodDelete || got.Path != "/api/v1/clusters/c1" {
		t.Fatalf("delete sent %s %s", got.Method, got.Path)
	}
}

func TestClustersKubeconfigIsRawYAML(t *testing.T) {
	const kc = "apiVersion: v1\nkind: Config\nclusters: []\n"
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(kc))
	})
	got, err := c.Clusters.Kubeconfig(context.Background(), "c1")
	if err != nil || got != kc {
		t.Fatalf("got %q err=%v", got, err)
	}
	if f.calls[0].Path != "/api/v1/clusters/c1/kubeconfig" {
		t.Fatal(f.calls[0].Path)
	}
}

func TestClustersKubeconfigNotActive(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		writeJSON(w, 400, map[string]any{"code": "bad_request", "detail": "Cluster is not active (status: provisioning)."})
	})
	if _, err := c.Clusters.Kubeconfig(context.Background(), "c1"); err == nil {
		t.Fatal("want an error")
	}
}

func TestClustersNodePools(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		pool := map[string]any{"id": "np2", "name": "big", "cluster_id": "c1", "size_slug": "k8s-4vcpu-8gb", "count": 3,
			"min_count": 3, "max_count": 3, "labels": map[string]string{"tier": "db"},
			"taints": []any{map[string]any{"key": "dedicated", "value": "db", "effect": "NoSchedule"}}, "status": "provisioning"}
		if r.Method == http.MethodGet {
			writeJSON(w, 200, []any{pool})
			return
		}
		writeJSON(w, 201, pool)
	})
	ctx := context.Background()
	three, db := 3, "db"
	pool, err := c.Clusters.CreateNodePool(ctx, "c1", CreateNodePoolRequest{
		Name: "big", NodeSize: "k8s-4vcpu-8gb", Count: &three, Labels: map[string]string{"tier": "db"},
		Taints: []Taint{{Key: "dedicated", Value: &db, Effect: "NoSchedule"}},
	})
	if err != nil || pool.Id != "np2" || pool.Taints[0].Effect != "NoSchedule" {
		t.Fatalf("pool=%v err=%v", pool, err)
	}
	got := f.calls[0]
	if got.Method != http.MethodPost || got.Path != "/api/v1/clusters/c1/nodepools" || got.Header.Get("Idempotency-Key") == "" {
		t.Fatalf("sent %s %s", got.Method, got.Path)
	}
	want := `{"name":"big","node_size":"k8s-4vcpu-8gb","count":3,"labels":{"tier":"db"},"taints":[{"key":"dedicated","value":"db","effect":"NoSchedule"}]}`
	if got.Body != want {
		t.Fatalf("body %s", got.Body)
	}
	pools, err := c.Clusters.ListNodePools(ctx, "c1")
	if err != nil || len(pools) != 1 || pools[0].Labels["tier"] != "db" {
		t.Fatalf("pools=%v err=%v", pools, err)
	}
}

func TestClustersWaitActive(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 1:
			writeJSON(w, 200, cluster("pending"))
		case 2:
			writeJSON(w, 200, cluster("provisioning"))
		default:
			writeJSON(w, 200, cluster("active"))
		}
	})
	cl, err := c.Clusters.WaitActive(context.Background(), "c1", &WaitOptions{Interval: time.Millisecond})
	if err != nil || cl.Status != ClusterActive {
		t.Fatalf("cl=%v err=%v", cl, err)
	}
}

func TestClustersWaitActiveStuck(t *testing.T) {
	c, _ := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		cl := cluster("error")
		cl["error_message"] = "no capacity"
		writeJSON(w, 200, cl)
	})
	_, err := c.Clusters.WaitActive(context.Background(), "c1", &WaitOptions{Interval: time.Millisecond})
	var stuck *ErrStuck
	if !errors.As(err, &stuck) || stuck.Detail != "no capacity" {
		t.Fatalf("err=%v", err)
	}
}

func TestClustersWaitDeleted(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		if n < 3 {
			writeJSON(w, 200, cluster("deleting"))
			return
		}
		writeJSON(w, 404, map[string]any{"code": "not_found", "detail": "Cluster not found"})
	})
	if err := c.Clusters.WaitDeleted(context.Background(), "c1", &WaitOptions{Interval: time.Millisecond}); err != nil || len(f.calls) != 3 {
		t.Fatalf("calls=%d err=%v", len(f.calls), err)
	}
}

func TestClustersUpdateAndNodePoolChanges(t *testing.T) {
	c, f := newFake(t, func(w http.ResponseWriter, r *http.Request, n int) {
		switch n {
		case 1:
			cl := cluster("active")
			cl["name"] = "prod-2"
			writeJSON(w, 200, cl)
		case 2:
			writeJSON(w, 200, map[string]any{"id": "np1", "name": "default", "cluster_id": "c1", "count": 4, "status": "scaling"})
		default:
			writeJSON(w, 200, map[string]any{"message": "Node pool deletion started"})
		}
	})
	ctx := context.Background()
	name := "prod-2"
	cl, err := c.Clusters.Update(ctx, "c1", UpdateClusterRequest{Name: &name})
	if err != nil || cl.Name != "prod-2" {
		t.Fatalf("cl=%v err=%v", cl, err)
	}
	if got := f.calls[0]; got.Method != http.MethodPatch || got.Path != "/api/v1/clusters/c1/settings" || got.Body != `{"name":"prod-2"}` {
		t.Fatalf("update sent %s %s %s", got.Method, got.Path, got.Body)
	}
	four := 4
	pool, err := c.Clusters.UpdateNodePool(ctx, "c1", "np1", UpdateNodePoolRequest{Count: &four})
	if err != nil || pool.Count != 4 {
		t.Fatalf("pool=%v err=%v", pool, err)
	}
	if got := f.calls[1]; got.Method != http.MethodPatch || got.Path != "/api/v1/clusters/c1/nodepools/np1" || got.Body != `{"count":4}` {
		t.Fatalf("resize sent %s %s %s", got.Method, got.Path, got.Body)
	}
	if err := c.Clusters.DeleteNodePool(ctx, "c1", "np1"); err != nil {
		t.Fatal(err)
	}
	if got := f.calls[2]; got.Method != http.MethodDelete || got.Path != "/api/v1/clusters/c1/nodepools/np1" {
		t.Fatalf("delete sent %s %s", got.Method, got.Path)
	}
}

func TestClusterWaitDefaultsTo15s(t *testing.T) {
	if o := clusterWait(nil); o.Interval != ClusterPollInterval {
		t.Fatal(o.Interval)
	}
	if o := clusterWait(&WaitOptions{Timeout: time.Minute}); o.Interval != ClusterPollInterval || o.Timeout != time.Minute {
		t.Fatal(o)
	}
	if o := clusterWait(&WaitOptions{Interval: time.Second}); o.Interval != time.Second {
		t.Fatal(o.Interval)
	}
}
