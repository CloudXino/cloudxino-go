package cloudxino

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// Server states (Server.Status).
const (
	ServerCreating  = "creating"
	ServerRunning   = "running"
	ServerStopped   = "stopped"
	ServerStarting  = "starting"
	ServerStopping  = "stopping"
	ServerRebooting = "rebooting"
	ServerResizing  = "resizing"
	ServerRestoring = "restoring"
	ServerDeleting  = "deleting"
	ServerError     = "error"
)

// ServersService manages CC2 servers (/v1/mc2).
type ServersService struct{ c *Client }

// ListServersOptions narrows List.
type ListServersOptions struct {
	ProjectID string
}

// ActionResult is what a power / resize action answers.
type ActionResult struct {
	Message string         `json:"message"`
	Status  string         `json:"status"`
	Resize  map[string]any `json:"resize,omitempty"`
}

func (s *ServersService) List(ctx context.Context, opts *ListServersOptions) ([]Server, error) {
	q := url.Values{}
	if opts != nil && opts.ProjectID != "" {
		q.Set("project_id", opts.ProjectID)
	}
	var out []Server
	err := s.c.do(ctx, request{method: http.MethodGet, path: "/v1/mc2", query: q}, &out)
	return out, err
}

func (s *ServersService) Get(ctx context.Context, id string) (*Server, error) {
	var out Server
	if err := s.c.do(ctx, request{method: http.MethodGet, path: "/v1/mc2" + p(id)}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Create orders a server. It answers at once with status "creating"; use WaitReady.
// idempotencyKey may be empty (one is made up); pass your own to make retries across restarts safe.
func (s *ServersService) Create(ctx context.Context, req CreateMC2Request, idempotencyKey ...string) (*Server, error) {
	var out Server
	r := request{method: http.MethodPost, path: "/v1/mc2", body: req}
	if len(idempotencyKey) > 0 {
		r.idempotencyKey = idempotencyKey[0]
	}
	if err := s.c.do(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Update changes the name and/or tags.
func (s *ServersService) Update(ctx context.Context, id string, req UpdateMC2Request) (*Server, error) {
	var out Server
	if err := s.c.do(ctx, request{method: http.MethodPatch, path: "/v1/mc2" + p(id), body: req}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete starts deleting the server; use WaitDeleted to know when it is gone.
func (s *ServersService) Delete(ctx context.Context, id string) error {
	return s.c.do(ctx, request{method: http.MethodDelete, path: "/v1/mc2" + p(id)}, nil)
}

func (s *ServersService) action(ctx context.Context, id, action string, body any) (*ActionResult, error) {
	var out ActionResult
	if err := s.c.do(ctx, request{method: http.MethodPost, path: "/v1/mc2" + p(id, action), body: body}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *ServersService) PowerOn(ctx context.Context, id string) (*ActionResult, error) {
	return s.action(ctx, id, "power-on", nil)
}

func (s *ServersService) PowerOff(ctx context.Context, id string) (*ActionResult, error) {
	return s.action(ctx, id, "power-off", nil)
}

func (s *ServersService) Reboot(ctx context.Context, id string) (*ActionResult, error) {
	return s.action(ctx, id, "reboot", nil)
}

// Resize moves the server to another size (disk can only grow); wait with WaitResized.
func (s *ServersService) Resize(ctx context.Context, id, size string) (*ActionResult, error) {
	return s.action(ctx, id, "resize", ResizeMC2Request{Size: size})
}

// PrivateKey downloads the platform-made SSH private key of a server created with login_method "key".
// Prefer account SSH keys (SSHKeys, CreateMC2Request.SshKeyIds): then no private key leaves your machine.
func (s *ServersService) PrivateKey(ctx context.Context, id string) ([]byte, error) {
	var raw []byte
	err := s.c.do(ctx, request{method: http.MethodGet, path: "/v1/mc2" + p(id, "ssh-key"), raw: &raw}, nil)
	return raw, err
}

// WaitReady waits until the server is running or stopped (after create, resize, power actions).
func (s *ServersService) WaitReady(ctx context.Context, id string, opts *WaitOptions) (*Server, error) {
	var last *Server
	err := waitFor(ctx, opts, func(ctx context.Context) (bool, error) {
		srv, err := s.Get(ctx, id)
		if err != nil {
			return false, err
		}
		last = srv
		switch strv(srv.Status) {
		case ServerRunning, ServerStopped:
			return true, nil
		case ServerError, ServerDeleting:
			return false, &ErrStuck{Kind: "server", ID: id, Status: strv(srv.Status)}
		}
		return false, nil
	})
	return last, err
}

// WaitDeleted waits until the server is gone (GET answers 404).
func (s *ServersService) WaitDeleted(ctx context.Context, id string, opts *WaitOptions) error {
	return waitFor(ctx, opts, func(ctx context.Context) (bool, error) {
		srv, err := s.Get(ctx, id)
		if IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		if strv(srv.Status) == ServerError {
			return false, &ErrStuck{Kind: "server", ID: id, Status: ServerError, Detail: "deletion failed"}
		}
		return false, nil
	})
}

// WaitResized waits until a resize to size has finished; it fails when the API reports the resize failed.
func (s *ServersService) WaitResized(ctx context.Context, id, size string, opts *WaitOptions) (*Server, error) {
	var last *Server
	err := waitFor(ctx, opts, func(ctx context.Context) (bool, error) {
		srv, err := s.Get(ctx, id)
		if err != nil {
			return false, err
		}
		last = srv
		if srv.Resize != nil {
			if msg, _ := (*srv.Resize)["error"].(string); msg != "" {
				return false, &ErrStuck{Kind: "server", ID: id, Status: "resize failed", Detail: msg}
			}
		}
		switch strv(srv.Status) {
		case ServerRunning, ServerStopped:
			return strv(srv.Size) == size, nil
		case ServerError, ServerDeleting:
			return false, &ErrStuck{Kind: "server", ID: id, Status: strv(srv.Status)}
		}
		return false, nil
	})
	return last, err
}

// Regions, Sizes and Images are what Create accepts. Their shape is the console's, so they are raw JSON.
func (s *ServersService) Regions(ctx context.Context) (json.RawMessage, error) {
	return s.options(ctx, "regions", nil)
}

func (s *ServersService) Sizes(ctx context.Context) (json.RawMessage, error) {
	return s.options(ctx, "sizes", nil)
}

func (s *ServersService) Images(ctx context.Context, region string) (json.RawMessage, error) {
	q := url.Values{}
	if region != "" {
		q.Set("region", region)
	}
	return s.options(ctx, "images", q)
}

func (s *ServersService) options(ctx context.Context, what string, q url.Values) (json.RawMessage, error) {
	var raw []byte
	err := s.c.do(ctx, request{method: http.MethodGet, path: "/v1/mc2/options/" + what, query: q, raw: &raw}, nil)
	return raw, err
}
