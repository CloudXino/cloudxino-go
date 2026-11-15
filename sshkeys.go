package cloudxino

import (
	"context"
	"net/http"
	"net/url"
)

// SSHKeysService manages the team's public SSH keys (/v1/ssh-keys). Put them on new servers with
// CreateMC2Request.SshKeyIds.
type SSHKeysService struct{ c *Client }

// ListSSHKeysOptions filters List by exact name or fingerprint ("SHA256:...").
type ListSSHKeysOptions struct {
	Name        string
	Fingerprint string
}

func (s *SSHKeysService) List(ctx context.Context, opts *ListSSHKeysOptions) ([]SSHKeyOut, error) {
	q := url.Values{}
	if opts != nil {
		if opts.Name != "" {
			q.Set("name", opts.Name)
		}
		if opts.Fingerprint != "" {
			q.Set("fingerprint", opts.Fingerprint)
		}
	}
	return getList[SSHKeyOut](ctx, s.c, "/v1/ssh-keys", q)
}

func (s *SSHKeysService) Get(ctx context.Context, id string) (*SSHKeyOut, error) {
	return getOne[SSHKeyOut](ctx, s.c, "/v1/ssh-keys"+p(id))
}

// Create saves a public key ("ssh-ed25519 AAAA... comment"). A key the team already has answers 409 already_exists.
func (s *SSHKeysService) Create(ctx context.Context, name, publicKey string, idempotencyKey ...string) (*SSHKeyOut, error) {
	return send[SSHKeyOut](ctx, s.c, http.MethodPost, "/v1/ssh-keys", SSHKeyCreate{Name: name, PublicKey: publicKey}, idempotencyKey)
}

// Rename changes a key's name (the key itself cannot change).
func (s *SSHKeysService) Rename(ctx context.Context, id, name string) (*SSHKeyOut, error) {
	return send[SSHKeyOut](ctx, s.c, http.MethodPatch, "/v1/ssh-keys"+p(id), SSHKeyUpdate{Name: &name}, nil)
}

// Delete forgets a key; servers that already have it keep it.
func (s *SSHKeysService) Delete(ctx context.Context, id string) error {
	return del(ctx, s.c, "/v1/ssh-keys"+p(id))
}
