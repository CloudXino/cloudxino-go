package cloudxino

import (
	"context"
	"net/http"
)

// ProjectsService manages projects (/v1/projects), which group a team's resources.
type ProjectsService struct{ c *Client }

func (s *ProjectsService) List(ctx context.Context) ([]Project, error) {
	return getList[Project](ctx, s.c, "/v1/projects", nil)
}

func (s *ProjectsService) Get(ctx context.Context, id string) (*Project, error) {
	return getOne[Project](ctx, s.c, "/v1/projects"+p(id))
}

func (s *ProjectsService) Create(ctx context.Context, req CreateProjectRequest, idempotencyKey ...string) (*Project, error) {
	return send[Project](ctx, s.c, http.MethodPost, "/v1/projects", req, idempotencyKey)
}

func (s *ProjectsService) Update(ctx context.Context, id string, req UpdateProjectRequest) (*Project, error) {
	return send[Project](ctx, s.c, http.MethodPatch, "/v1/projects"+p(id), req, nil)
}

func (s *ProjectsService) Delete(ctx context.Context, id string) error {
	return del(ctx, s.c, "/v1/projects"+p(id))
}
