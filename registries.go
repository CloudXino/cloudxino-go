package cloudxino

import (
	"context"
	"net/url"
)

// Container Hub registry states (ContainerRegistry.Status).
const (
	RegistryCreating = "creating"
	RegistryActive   = "active"
)

// ContainerRegistry is a Container Hub (CCH) registry: a private image registry at Endpoint
// (e.g. "registry.example.com/my-hub"). Written by hand like containerhosts.go. The API also
// answers with the robot login; the SDK leaves it out, since nothing here needs it.
type ContainerRegistry struct {
	Id        string  `json:"id"`
	Name      string  `json:"name"`
	Region    string  `json:"region"`
	Endpoint  string  `json:"endpoint"` // push and pull images as <Endpoint>/<image>:<tag>
	Status    string  `json:"status"`   // creating, active, ...
	ProjectId *string `json:"project_id,omitempty"`
	CreatedAt *string `json:"created_at,omitempty"`
}

// ListRegistriesOptions filters Registries.List by project.
type ListRegistriesOptions struct {
	ProjectID string
}

// RegistriesService reads Container Hub registries (/v1/registries). They are made in the console.
type RegistriesService struct{ c *Client }

func (s *RegistriesService) List(ctx context.Context, opts *ListRegistriesOptions) ([]ContainerRegistry, error) {
	q := url.Values{}
	if opts != nil && opts.ProjectID != "" {
		q.Set("project_id", opts.ProjectID)
	}
	return getList[ContainerRegistry](ctx, s.c, "/v1/registries", q)
}

func (s *RegistriesService) Get(ctx context.Context, id string) (*ContainerRegistry, error) {
	return getOne[ContainerRegistry](ctx, s.c, "/v1/registries"+p(id))
}
