package cloudxino

import (
	"context"
	"net/http"
	"net/url"
)

func getOne[T any](ctx context.Context, c *Client, path string) (*T, error) {
	var out T
	if err := c.do(ctx, request{method: http.MethodGet, path: path}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func getList[T any](ctx context.Context, c *Client, path string, q url.Values) ([]T, error) {
	var out []T
	err := c.do(ctx, request{method: http.MethodGet, path: path, query: q}, &out)
	return out, err
}

func send[T any](ctx context.Context, c *Client, method, path string, body any, idempotencyKey []string) (*T, error) {
	var out T
	r := request{method: method, path: path, body: body}
	if len(idempotencyKey) > 0 {
		r.idempotencyKey = idempotencyKey[0]
	}
	if err := c.do(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func del(ctx context.Context, c *Client, path string) error {
	return c.do(ctx, request{method: http.MethodDelete, path: path}, nil)
}
