// Package cloudxino is the Go client for the CloudXino cloud API.
//
//	client, err := cloudxino.NewClient(os.Getenv("CLOUDXINO_TOKEN"))
//	server, err := client.Servers.Create(ctx, cloudxino.CreateMC2Request{Name: "web-01", Image: "debian-13", Size: "s-1vcpu-1gb"})
//	server, err = client.Servers.WaitReady(ctx, server.Id)
//
// Requests are retried on rate limits (honouring Retry-After), maintenance and gateway errors.
// Creates carry an Idempotency-Key, so a retried create never makes a second resource.
package cloudxino

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	mrand "math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Version of this SDK, sent in the User-Agent.
const Version = "0.1.0"

// DefaultBaseURL is the public API.
const DefaultBaseURL = "https://cloudxino.com/api"

// Client talks to the CloudXino API with one API key (a team's key, Account → API keys).
type Client struct {
	baseURL    *url.URL
	token      string
	httpClient *http.Client
	userAgent  string
	maxRetries int
	minBackoff time.Duration
	maxBackoff time.Duration

	Servers        *ServersService
	Firewalls      *FirewallsService
	LoadBalancers  *LoadBalancersService
	DNS            *DNSService
	Projects       *ProjectsService
	SSHKeys        *SSHKeysService
	ReservedIPs    *ReservedIPsService
	Clusters       *ClustersService
	ContainerHosts *ContainerHostsService
	Registries     *RegistriesService
	Account        *AccountService
}

// Option changes a Client made by NewClient.
type Option func(*Client) error

// WithBaseURL points the client at another API address, e.g. "https://console.example.com/api".
func WithBaseURL(raw string) Option {
	return func(c *Client) error {
		u, err := url.Parse(strings.TrimRight(raw, "/"))
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("cloudxino: invalid base URL %q", raw)
		}
		c.baseURL = u
		return nil
	}
}

// WithHTTPClient uses hc for every request.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) error { c.httpClient = hc; return nil }
}

// WithUserAgent puts product/version in front of the SDK's own User-Agent (e.g. "terraform-provider-cloudxino/0.1.0").
func WithUserAgent(ua string) Option {
	return func(c *Client) error { c.userAgent = ua + " " + c.userAgent; return nil }
}

// WithRetries sets how often a retryable request is tried again (default 5; 0 turns retries off)
// and the backoff bounds.
func WithRetries(max int, minBackoff, maxBackoff time.Duration) Option {
	return func(c *Client) error {
		c.maxRetries, c.minBackoff, c.maxBackoff = max, minBackoff, maxBackoff
		return nil
	}
}

// NewClient makes a client for the API key token ("cxk_...").
func NewClient(token string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("cloudxino: an API key is required (Account → API keys in the console)")
	}
	base, _ := url.Parse(DefaultBaseURL)
	c := &Client{
		baseURL:    base,
		token:      strings.TrimSpace(token),
		httpClient: &http.Client{Timeout: 60 * time.Second},
		userAgent:  "cloudxino-go/" + Version,
		maxRetries: 5,
		minBackoff: 500 * time.Millisecond,
		maxBackoff: 30 * time.Second,
	}
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return nil, err
		}
	}
	c.Servers = &ServersService{c}
	c.Firewalls = &FirewallsService{c}
	c.LoadBalancers = &LoadBalancersService{c}
	c.DNS = &DNSService{c}
	c.Projects = &ProjectsService{c}
	c.SSHKeys = &SSHKeysService{c}
	c.ReservedIPs = &ReservedIPsService{c}
	c.Clusters = &ClustersService{c}
	c.ContainerHosts = &ContainerHostsService{c}
	c.Registries = &RegistriesService{c}
	c.Account = &AccountService{c}
	return c, nil
}

// request is one API call.
type request struct {
	method string
	path   string // below the base URL, e.g. "/v1/mc2"
	query  url.Values
	body   any
	// idempotencyKey is sent on POST so the call can be retried safely; empty = one is made up.
	idempotencyKey string
	// raw answers are written here instead of decoding JSON
	raw *[]byte
}

// NewIdempotencyKey returns a random key for Idempotency-Key. Keep the same key when you retry
// the same create yourself (e.g. across process restarts) to get the first answer back.
func NewIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (c *Client) do(ctx context.Context, r request, out any) error {
	var payload []byte
	if r.body != nil {
		var err error
		if payload, err = json.Marshal(r.body); err != nil {
			return fmt.Errorf("cloudxino: encoding request: %w", err)
		}
	}
	// r.path is already escaped (see p), so it is joined as text, not set as url.URL.Path
	target := strings.TrimRight(c.baseURL.String(), "/") + r.path
	if len(r.query) > 0 {
		target += "?" + r.query.Encode()
	}
	if r.method == http.MethodPost && r.idempotencyKey == "" {
		r.idempotencyKey = NewIdempotencyKey()
	}
	// POST is safe to resend only with an Idempotency-Key, which every POST here has
	for attempt := 0; ; attempt++ {
		var body io.Reader
		if payload != nil {
			body = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, r.method, target, body)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if r.idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", r.idempotencyKey)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if attempt < c.maxRetries {
				if werr := c.sleep(ctx, attempt, 0); werr != nil {
					return werr
				}
				continue
			}
			return fmt.Errorf("cloudxino: %s %s: %w", r.method, r.path, err)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("cloudxino: reading answer: %w", err)
		}
		if resp.StatusCode >= 400 {
			apiErr := parseError(resp, data)
			if apiErr.Retryable() && attempt < c.maxRetries {
				if werr := c.sleep(ctx, attempt, apiErr.RetryAfter); werr != nil {
					return werr
				}
				continue
			}
			return apiErr
		}
		if r.raw != nil {
			*r.raw = data
			return nil
		}
		if out == nil || len(data) == 0 || resp.StatusCode == http.StatusNoContent {
			return nil
		}
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("cloudxino: decoding %s %s: %w", r.method, r.path, err)
		}
		return nil
	}
}

func (c *Client) sleep(ctx context.Context, attempt int, retryAfter time.Duration) error {
	wait := retryAfter
	if wait <= 0 {
		backoff := float64(c.minBackoff) * math.Pow(2, float64(attempt))
		if backoff > float64(c.maxBackoff) {
			backoff = float64(c.maxBackoff)
		}
		wait = time.Duration(backoff/2 + mrand.Float64()*backoff/2) // jitter
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return time.Until(t)
	}
	return 0
}

// p joins path segments, escaping each, so an ID can never reach another route.
func p(segments ...string) string {
	var b strings.Builder
	for _, s := range segments {
		b.WriteString("/")
		b.WriteString(url.PathEscape(s))
	}
	return b.String()
}
