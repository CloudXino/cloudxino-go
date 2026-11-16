package cloudxino

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// Cluster states (Cluster.Status).
const (
	ClusterPending      = "pending"
	ClusterProvisioning = "provisioning"
	ClusterActive       = "active"
	ClusterUpdating     = "updating" // a node pool is being added or resized
	ClusterDeleting     = "deleting"
	ClusterError        = "error"
)

// The cluster and node pool answers are free-form on the API side (no OpenAPI schema), so
// these types are written by hand instead of generated into types.gen.go.

// Cluster is a CKS (managed Kubernetes) cluster. Get fills every field; List and Create leave
// out the endpoint and the ingress switch.
type Cluster struct {
	Id                string            `json:"id"`
	Name              string            `json:"name"`
	KubernetesVersion string            `json:"kubernetes_version"`
	Region            string            `json:"region"`
	Status            string            `json:"status"`
	ProjectId         string            `json:"project_id"`
	OrganizationId    string            `json:"organization_id"`
	ErrorMessage      *string           `json:"error_message,omitempty"`
	ApiEndpoint       *string           `json:"api_endpoint,omitempty"` // https://host:port for kubectl, once provisioned
	Ingress           *bool             `json:"ingress,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
	NodePools         []ClusterNodePool `json:"node_pools,omitempty"` // List only
	Progress          map[string]any    `json:"progress,omitempty"`
	CreatedAt         *string           `json:"created_at,omitempty"`
	Message           *string           `json:"message,omitempty"` // Create only
}

// ClusterNodePool is the short pool view in a List answer.
type ClusterNodePool struct {
	Id     string `json:"id"`
	Name   string `json:"name"`
	Cpu    int    `json:"cpu"`
	Memory int    `json:"memory"`
	Disk   int    `json:"disk"`
	Count  int    `json:"count"`
	Status string `json:"status"`
}

// NodePool is a group of worker nodes of one size.
type NodePool struct {
	Id          string            `json:"id"`
	Name        string            `json:"name"`
	ClusterId   string            `json:"cluster_id"`
	SizeSlug    *string           `json:"size_slug,omitempty"`
	Cpu         int               `json:"cpu"`
	Memory      int               `json:"memory"` // GB
	Disk        int               `json:"disk"`   // GB
	Count       int               `json:"count"`
	MinCount    int               `json:"min_count"`
	MaxCount    int               `json:"max_count"`
	AutoScaling bool              `json:"auto_scaling"`
	Labels      map[string]string `json:"labels,omitempty"`
	Taints      []Taint           `json:"taints,omitempty"`
	Status      string            `json:"status"`
	CreatedAt   *string           `json:"created_at,omitempty"`
	UpdatedAt   *string           `json:"updated_at,omitempty"`
}

// Taint keeps pods off a pool's nodes unless they tolerate it.
type Taint struct {
	Key    string  `json:"key"`
	Value  *string `json:"value,omitempty"`
	Effect string  `json:"effect"` // NoSchedule, PreferNoSchedule or NoExecute
}

// NodePoolAutoscaling lets the autoscaler move a pool between Min and Max nodes.
type NodePoolAutoscaling struct {
	Enabled bool `json:"enabled"`
	Min     int  `json:"min,omitempty"`
	Max     int  `json:"max,omitempty"`
}

// CreateClusterRequest creates a cluster with one node pool named "default".
type CreateClusterRequest struct {
	Name string `json:"name"`
	// KubernetesVersion e.g. "1.36.5"; nil = the newest offered.
	KubernetesVersion *string `json:"kubernetes_version,omitempty"`
	// Region ID; nil = the default region.
	Region *string `json:"region,omitempty"`
	// NodeSize is a worker size slug, e.g. "k8s-2vcpu-4gb" (GET /v1/pricing → cluster.node_sizes).
	NodeSize string `json:"node_size"`
	// NodeCount of the default pool; nil = 2.
	NodeCount *int `json:"node_count,omitempty"`
	// Ingress installs Traefik as the cluster's one LoadBalancer; nil = true.
	Ingress   *bool   `json:"ingress,omitempty"`
	VpcId     *string `json:"vpc_id,omitempty"`
	ProjectId *string `json:"project_id,omitempty"` // nil = the team's default project
}

// CreateNodePoolRequest adds a node pool to an active cluster.
type CreateNodePoolRequest struct {
	Name        string               `json:"name"` // lowercase, digits, '-'; unique in the cluster
	NodeSize    string               `json:"node_size"`
	Count       *int                 `json:"count,omitempty"` // nil = 2
	Labels      map[string]string    `json:"labels,omitempty"`
	Taints      []Taint              `json:"taints,omitempty"`
	Autoscaling *NodePoolAutoscaling `json:"autoscaling,omitempty"`
}

// UpdateClusterRequest changes the cluster's settings; nil fields stay as they are.
type UpdateClusterRequest struct {
	Name      *string   `json:"name,omitempty"`
	ProjectId *string   `json:"project_id,omitempty"`
	Tags      *[]string `json:"tags,omitempty"`
}

// UpdateNodePoolRequest resizes a pool and/or replaces its labels and taints; nil fields stay.
type UpdateNodePoolRequest struct {
	Count       *int                 `json:"count,omitempty"`
	Labels      *map[string]string   `json:"labels,omitempty"`
	Taints      *[]Taint             `json:"taints,omitempty"`
	Autoscaling *NodePoolAutoscaling `json:"autoscaling,omitempty"`
}

// ListClustersOptions filters List by project.
type ListClustersOptions struct {
	ProjectID string
}

// ClustersService manages CKS clusters (/v1/clusters): a managed control plane plus worker
// node pools on the team's private network. Billed per hour (control plane + nodes) until deleted.
//
// The API answers a cluster's Get and ListNodePools at most once per 15 seconds per user (429
// with Retry-After, which the client waits out), so the cluster waiters poll every 15s by default.
type ClustersService struct{ c *Client }

// ClusterPollInterval is the waiters' default interval: the API's limit for reading one cluster.
const ClusterPollInterval = 15 * time.Second

func clusterWait(opts *WaitOptions) *WaitOptions {
	o := WaitOptions{Interval: ClusterPollInterval}
	if opts != nil {
		o = *opts
		if o.Interval <= 0 {
			o.Interval = ClusterPollInterval
		}
	}
	return &o
}

func (s *ClustersService) List(ctx context.Context, opts *ListClustersOptions) ([]Cluster, error) {
	q := url.Values{}
	if opts != nil && opts.ProjectID != "" {
		q.Set("project_id", opts.ProjectID)
	}
	return getList[Cluster](ctx, s.c, "/v1/clusters", q)
}

func (s *ClustersService) Get(ctx context.Context, id string) (*Cluster, error) {
	return getOne[Cluster](ctx, s.c, "/v1/clusters"+p(id))
}

// Create starts building the cluster (usually about 5 minutes); WaitActive waits for it.
// When CKS is switched off on the platform it answers 501.
func (s *ClustersService) Create(ctx context.Context, req CreateClusterRequest, idempotencyKey ...string) (*Cluster, error) {
	return send[Cluster](ctx, s.c, http.MethodPost, "/v1/clusters", req, idempotencyKey)
}

// Update renames the cluster, moves it to another project or replaces its tags.
func (s *ClustersService) Update(ctx context.Context, id string, req UpdateClusterRequest) (*Cluster, error) {
	return send[Cluster](ctx, s.c, http.MethodPatch, "/v1/clusters"+p(id, "settings"), req, nil)
}

// Delete starts removing the cluster and its nodes; WaitDeleted waits until it is gone.
func (s *ClustersService) Delete(ctx context.Context, id string) error {
	return del(ctx, s.c, "/v1/clusters"+p(id))
}

// Kubeconfig returns the admin kubeconfig (YAML). The API gives it only while the cluster is active.
func (s *ClustersService) Kubeconfig(ctx context.Context, id string) (string, error) {
	var raw []byte
	if err := s.c.do(ctx, request{method: http.MethodGet, path: "/v1/clusters" + p(id, "kubeconfig"), raw: &raw}, nil); err != nil {
		return "", err
	}
	return string(raw), nil
}

func (s *ClustersService) ListNodePools(ctx context.Context, clusterID string) ([]NodePool, error) {
	return getList[NodePool](ctx, s.c, "/v1/clusters"+p(clusterID, "nodepools"), nil)
}

// CreateNodePool adds a pool; the cluster is "updating" until its nodes have joined.
func (s *ClustersService) CreateNodePool(ctx context.Context, clusterID string, req CreateNodePoolRequest, idempotencyKey ...string) (*NodePool, error) {
	return send[NodePool](ctx, s.c, http.MethodPost, "/v1/clusters"+p(clusterID, "nodepools"), req, idempotencyKey)
}

// UpdateNodePool resizes a pool (nodes are added before any are removed) and/or replaces its
// labels and taints. The cluster must be active; it is "updating" until the change is done.
func (s *ClustersService) UpdateNodePool(ctx context.Context, clusterID, poolID string, req UpdateNodePoolRequest) (*NodePool, error) {
	return send[NodePool](ctx, s.c, http.MethodPatch, "/v1/clusters"+p(clusterID, "nodepools", poolID), req, nil)
}

// DeleteNodePool starts removing a pool and its nodes.
func (s *ClustersService) DeleteNodePool(ctx context.Context, clusterID, poolID string) error {
	return del(ctx, s.c, "/v1/clusters"+p(clusterID, "nodepools", poolID))
}

// WaitActive waits until the cluster is active (built, or done changing a node pool).
func (s *ClustersService) WaitActive(ctx context.Context, id string, opts *WaitOptions) (*Cluster, error) {
	var last *Cluster
	err := waitFor(ctx, clusterWait(opts), func(ctx context.Context) (bool, error) {
		cl, err := s.Get(ctx, id)
		if err != nil {
			return false, err
		}
		last = cl
		switch cl.Status {
		case ClusterActive:
			return true, nil
		case ClusterError, ClusterDeleting:
			return false, &ErrStuck{Kind: "cluster", ID: id, Status: cl.Status, Detail: strv(cl.ErrorMessage)}
		}
		return false, nil
	})
	return last, err
}

// WaitDeleted waits until the cluster is gone.
func (s *ClustersService) WaitDeleted(ctx context.Context, id string, opts *WaitOptions) error {
	return waitFor(ctx, clusterWait(opts), func(ctx context.Context) (bool, error) {
		cl, err := s.Get(ctx, id)
		if IsNotFound(err) {
			return true, nil
		}
		if err == nil && cl.Status == ClusterError {
			return false, &ErrStuck{Kind: "cluster", ID: id, Status: cl.Status, Detail: strv(cl.ErrorMessage)}
		}
		return false, err
	})
}
