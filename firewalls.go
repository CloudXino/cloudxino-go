package cloudxino

import (
	"context"
	"net/http"
)

// FirewallsService manages Cloud Firewalls (/v1/firewalls). A firewall applies to the servers in
// server_ids and to every server carrying one of its tags. Rules are always sent as complete lists.
type FirewallsService struct{ c *Client }

func (s *FirewallsService) List(ctx context.Context) ([]Firewall, error) {
	return getList[Firewall](ctx, s.c, "/v1/firewalls", nil)
}

func (s *FirewallsService) Get(ctx context.Context, id string) (*Firewall, error) {
	return getOne[Firewall](ctx, s.c, "/v1/firewalls"+p(id))
}

// Create makes a firewall. Leaving OutboundRules nil allows all outbound traffic.
func (s *FirewallsService) Create(ctx context.Context, req CreateFirewallRequest, idempotencyKey ...string) (*Firewall, error) {
	return send[Firewall](ctx, s.c, http.MethodPost, "/v1/firewalls", req, idempotencyKey)
}

// Update replaces the fields that are set (a set rules list replaces all rules of that direction).
func (s *FirewallsService) Update(ctx context.Context, id string, req UpdateFirewallRequest) (*Firewall, error) {
	return send[Firewall](ctx, s.c, http.MethodPatch, "/v1/firewalls"+p(id), req, nil)
}

func (s *FirewallsService) Delete(ctx context.Context, id string) error {
	return del(ctx, s.c, "/v1/firewalls"+p(id))
}
