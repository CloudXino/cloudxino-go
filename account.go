package cloudxino

import (
	"context"
	"net/http"
)

// AccountService tells who the API key acts for.
type AccountService struct{ c *Client }

// Me is the signed-in identity behind the API key (GET /v1/users/me).
type Me struct {
	ID           string `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	Organization *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Role string `json:"role"`
	} `json:"organization"`
	APIKey *struct {
		Name      string  `json:"name"`
		KeyPrefix string  `json:"key_prefix"`
		Role      string  `json:"role"`
		RoleLabel string  `json:"role_label"`
		ExpiresAt *string `json:"expires_at"`
	} `json:"api_key"`
}

func (s *AccountService) Me(ctx context.Context) (*Me, error) {
	var out Me
	if err := s.c.do(ctx, request{method: http.MethodGet, path: "/v1/users/me"}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
