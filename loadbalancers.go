package cloudxino

import (
	"context"
	"net/http"
)

// Load balancer states (LoadBalancer.Status).
const (
	LoadBalancerCreating = "creating"
	LoadBalancerActive   = "active"
	LoadBalancerUpdating = "updating"
	LoadBalancerError    = "error"
	LoadBalancerDeleting = "deleting"
)

// LoadBalancersService manages CC2 load balancers (/v1/load-balancers).
type LoadBalancersService struct{ c *Client }

func (s *LoadBalancersService) List(ctx context.Context) ([]LoadBalancer, error) {
	return getList[LoadBalancer](ctx, s.c, "/v1/load-balancers", nil)
}

func (s *LoadBalancersService) Get(ctx context.Context, id string) (*LoadBalancer, error) {
	return getOne[LoadBalancer](ctx, s.c, "/v1/load-balancers"+p(id))
}

func (s *LoadBalancersService) Create(ctx context.Context, req CreateLoadBalancerRequest, idempotencyKey ...string) (*LoadBalancer, error) {
	return send[LoadBalancer](ctx, s.c, http.MethodPost, "/v1/load-balancers", req, idempotencyKey)
}

// Update replaces the fields that are set (TargetIds replaces the whole target list).
func (s *LoadBalancersService) Update(ctx context.Context, id string, req UpdateLoadBalancerRequest) (*LoadBalancer, error) {
	return send[LoadBalancer](ctx, s.c, http.MethodPatch, "/v1/load-balancers"+p(id), req, nil)
}

func (s *LoadBalancersService) AddTarget(ctx context.Context, id, serverID string) (*LoadBalancer, error) {
	return send[LoadBalancer](ctx, s.c, http.MethodPost, "/v1/load-balancers"+p(id, "targets"), TargetRequest{ServerId: serverID}, nil)
}

func (s *LoadBalancersService) RemoveTarget(ctx context.Context, id, serverID string) (*LoadBalancer, error) {
	return send[LoadBalancer](ctx, s.c, http.MethodDelete, "/v1/load-balancers"+p(id, "targets", serverID), nil, nil)
}

func (s *LoadBalancersService) Delete(ctx context.Context, id string) error {
	return del(ctx, s.c, "/v1/load-balancers"+p(id))
}

// WaitActive waits until the load balancer is active.
func (s *LoadBalancersService) WaitActive(ctx context.Context, id string, opts *WaitOptions) (*LoadBalancer, error) {
	var last *LoadBalancer
	err := waitFor(ctx, opts, func(ctx context.Context) (bool, error) {
		lb, err := s.Get(ctx, id)
		if err != nil {
			return false, err
		}
		last = lb
		switch strv(lb.Status) {
		case LoadBalancerActive:
			return true, nil
		case LoadBalancerError, LoadBalancerDeleting:
			return false, &ErrStuck{Kind: "load balancer", ID: id, Status: strv(lb.Status), Detail: strv(lb.StatusDetail)}
		}
		return false, nil
	})
	return last, err
}

// WaitDeleted waits until the load balancer is gone.
func (s *LoadBalancersService) WaitDeleted(ctx context.Context, id string, opts *WaitOptions) error {
	return waitFor(ctx, opts, func(ctx context.Context) (bool, error) {
		_, err := s.Get(ctx, id)
		if IsNotFound(err) {
			return true, nil
		}
		return false, err
	})
}
