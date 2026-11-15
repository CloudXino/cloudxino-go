package cloudxino

import (
	"context"
	"net/http"
	"net/url"
)

// ReservedIPsService manages reserved IPs (/v1/reserved-ips): public IPv4 addresses the team
// keeps until it deletes them, and points at a CC2 server in the same region. Moving one to
// another server keeps the address; nothing changes inside either server.
//
// When no address is free, or reserved IPs are turned off, Create and Assign answer 503 with
// code "unavailable" (IsUnavailable); that error is not retried.
type ReservedIPsService struct{ c *Client }

// ListReservedIPsOptions filters List by exact address ("203.0.113.10").
type ListReservedIPsOptions struct {
	IPAddress string
}

// Reserved IP status values.
const (
	ReservedIPStatusAvailable = "available" // reserved, not pointing anywhere
	ReservedIPStatusAssigned  = "assigned"  // pointing at a server
	ReservedIPStatusDeleting  = "deleting"
)

func (s *ReservedIPsService) List(ctx context.Context, opts *ListReservedIPsOptions) ([]ReservedIP, error) {
	q := url.Values{}
	if opts != nil && opts.IPAddress != "" {
		q.Set("ip_address", opts.IPAddress)
	}
	return getList[ReservedIP](ctx, s.c, "/v1/reserved-ips", q)
}

func (s *ReservedIPsService) Get(ctx context.Context, id string) (*ReservedIP, error) {
	return getOne[ReservedIP](ctx, s.c, "/v1/reserved-ips"+p(id))
}

// Create reserves an address. An empty Region means the default region, an empty ProjectId the
// Default project; with ServerId the address points at that server right away. Billed per hour
// until deleted.
func (s *ReservedIPsService) Create(ctx context.Context, req CreateReservedIPRequest, idempotencyKey ...string) (*ReservedIP, error) {
	return send[ReservedIP](ctx, s.c, http.MethodPost, "/v1/reserved-ips", req, idempotencyKey)
}

// Assign points the address at a CC2 server in its region, moving it when it points elsewhere.
// The change is done when it answers.
func (s *ReservedIPsService) Assign(ctx context.Context, id, serverID string) (*ReservedIP, error) {
	return send[ReservedIP](ctx, s.c, http.MethodPost, "/v1/reserved-ips"+p(id)+"/assign", AssignReservedIPRequest{ServerId: serverID}, nil)
}

// Unassign stops sending the address anywhere. The team keeps it (and is billed for it).
func (s *ReservedIPsService) Unassign(ctx context.Context, id string) (*ReservedIP, error) {
	return send[ReservedIP](ctx, s.c, http.MethodPost, "/v1/reserved-ips"+p(id)+"/unassign", nil, nil)
}

// Delete gives the address back, unassigning it first when it is assigned. Billing stops.
func (s *ReservedIPsService) Delete(ctx context.Context, id string) error {
	return del(ctx, s.c, "/v1/reserved-ips"+p(id))
}
