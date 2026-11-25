package cloudxino

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Container host states (ContainerHost.Status).
const (
	ContainerHostCreating   = "creating"   // the CC2 server is being made
	ContainerHostInstalling = "installing" // Docker, Traefik and Portainer are being installed
	ContainerHostReady      = "ready"
	ContainerHostFailed     = "failed" // see Error and LogTail; Retry runs the installer again
	ContainerHostDeleting   = "deleting"
	ContainerHostDeleted    = "deleted"
)

// Deployment states (ContainerDeployment.Status).
const (
	DeploymentRunning   = "running"
	DeploymentSucceeded = "succeeded"
	DeploymentFailed    = "failed"
)

// Container actions (ContainerHosts.ContainerAction).
const (
	ContainerStart   = "start"
	ContainerStop    = "stop" // refused for the host's own Traefik and Portainer containers
	ContainerRestart = "restart"
)

// Certificate states of a domain (ContainerDomainStatus.SSL).
const (
	DomainSSLActive     = "active"      // Let's Encrypt issued the certificate
	DomainSSLWaitingDNS = "waiting_dns" // the domain's A record does not point at the host yet
	DomainSSLPending    = "pending"     // DNS is right; the certificate is on its way
)

// The container host answers were not in the published OpenAPI document when this was written,
// so these types are written by hand (like clusters.go) instead of generated into types.gen.go.
// They match app/schemas/public.py in the console (ContainerHost, ContainerDeployment, ...).

// ContainerHostPlan is a plan offered for new hosts: a CC2 size plus the management fee. Prices
// are in BDT.
type ContainerHostPlan struct {
	Slug           string  `json:"slug"` // CC2 size slug, e.g. "s-2vcpu-4gb"
	Cpu            int     `json:"cpu"`
	MemoryGb       float64 `json:"memory_gb"`
	DiskGb         int     `json:"disk_gb"`
	ServerPerMonth float64 `json:"server_per_month"`
	FeePerMonth    float64 `json:"fee_per_month"`
	PerMonth       float64 `json:"per_month"`
	PerHour        float64 `json:"per_hour"`
	// The same plan on a NAT host (IpType "nat": no public IP). Zero when the console does not
	// say (before NAT hosts existed).
	NatServerPerMonth float64 `json:"nat_server_per_month,omitempty"`
	NatPerMonth       float64 `json:"nat_per_month,omitempty"`
	NatPerHour        float64 `json:"nat_per_hour,omitempty"`
}

// Container host IP types (CreateContainerHostRequest.IpType, ContainerHost.IpType).
const (
	ContainerHostIpPublic = "public" // the server has its own public IPv4
	ContainerHostIpNat    = "nat"    // no public IP; domains reach it through the shared NAT web proxy
)

// ContainerHostNat is how a NAT host is reached: visitors come to WebIp on 80/443, and the
// proxy passes each request on by its Host header / TLS SNI.
type ContainerHostNat struct {
	WebIp      string  `json:"web_ip"`             // point the A records of the host's domains here
	WebHost    *string `json:"web_host,omitempty"` // the proxy's host name, a CNAME target for subdomains
	PrivateIp  *string `json:"private_ip,omitempty"`
	MaxDomains int     `json:"max_domains"` // domains a NAT host can serve
	Ports      []int   `json:"ports,omitempty"`
}

// ContainerHostOptions says whether new hosts can be ordered and which plans there are.
type ContainerHostOptions struct {
	Enabled bool                `json:"enabled"`
	Plans   []ContainerHostPlan `json:"plans"`
	IpTypes []string            `json:"ip_types,omitempty"` // "public", and "nat" when NAT hosts can be ordered
	Nat     *ContainerHostNat   `json:"nat,omitempty"`      // set when NAT hosts can be ordered
}

// ContainerHostServer is the CC2 server under a host.
type ContainerHostServer struct {
	Status    string `json:"status"` // the CC2 server status: creating, running, resizing, ...
	IpAddress string `json:"ip_address"`
	Region    string `json:"region"`
	Cpu       int    `json:"cpu"`
	Memory    int    `json:"memory"` // MB
	Disk      int    `json:"disk"`   // GB
}

// ContainerHostPrice is what the host costs per month (server + management fee), in BDT.
type ContainerHostPrice struct {
	ServerPerMonth float64 `json:"server_per_month"`
	FeePerMonth    float64 `json:"fee_per_month"`
	PerMonth       float64 `json:"per_month"`
}

// ContainerHostResize is a plan change in progress, or the last one that failed.
type ContainerHostResize struct {
	To    string  `json:"to"`
	Error *string `json:"error,omitempty"`
}

// ContainerDomain routes a domain to a compose service's port through Traefik, with a Let's
// Encrypt certificate. The domain's A record must point at the host's IngressIp.
type ContainerDomain struct {
	Domain  string `json:"domain"`
	Service string `json:"service"` // a service in the compose file
	Port    int    `json:"port"`    // the port the service listens on inside its container
}

// ContainerDeployment is one `docker compose up` on a host. Compose and Output are filled by
// Deploy, GetDeployment and Redeploy; lists leave them out.
type ContainerDeployment struct {
	Id         string            `json:"id"`
	Number     int               `json:"number"` // 1, 2, ... per host
	Status     string            `json:"status"` // running, succeeded or failed
	Note       *string           `json:"note,omitempty"`
	EnvKeys    []string          `json:"env_keys"` // names of the env vars it ran with, never values
	Domains    []ContainerDomain `json:"domains"`
	CreatedBy  *string           `json:"created_by,omitempty"`
	StartedAt  *string           `json:"started_at,omitempty"`
	FinishedAt *string           `json:"finished_at,omitempty"`
	Compose    *string           `json:"compose,omitempty"`
	Output     *string           `json:"output,omitempty"` // the end of the compose output
}

// ContainerHostFirewall is a Cloud Firewall that applies to the host's server.
type ContainerHostFirewall struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

// ContainerHost is a Managed Container Service host: a CC2 server of the team's own with Docker,
// Compose, Traefik (Let's Encrypt) and Portainer. Get, Create, Retry and ChangePlan fill every
// field; List leaves out the ones marked "Get only".
type ContainerHost struct {
	Id             string               `json:"id"`
	Name           string               `json:"name"`
	Plan           string               `json:"plan"`                 // CC2 size slug
	IpType         string               `json:"ip_type"`              // public or nat (ContainerHostIp*); "" from consoles before NAT hosts
	IngressIp      *string              `json:"ingress_ip,omitempty"` // point domains' A records here: the server's IP, or the NAT web proxy's
	Nat            *ContainerHostNat    `json:"nat,omitempty"`        // NAT hosts only
	Status         string               `json:"status"`               // creating, installing, ready, failed, deleting, deleted
	Error          *string              `json:"error,omitempty"`
	ProjectId      string               `json:"project_id"`
	InstanceId     string               `json:"instance_id"` // the CC2 server
	Server         ContainerHostServer  `json:"server"`
	PortainerUrl   string               `json:"portainer_url"`
	Domains        []ContainerDomain    `json:"domains"`
	RegistryIds    []string             `json:"registry_ids"` // Container Hub registries logged in to before pulling
	Price          *ContainerHostPrice  `json:"price,omitempty"`
	CreatedAt      *string              `json:"created_at,omitempty"`
	ReadyAt        *string              `json:"ready_at,omitempty"`
	LastDeployment *ContainerDeployment `json:"last_deployment,omitempty"`

	Firewalls    []ContainerHostFirewall `json:"firewalls,omitempty"`     // Get only
	BackupPolicy map[string]any          `json:"backup_policy,omitempty"` // Get only
	Resize       *ContainerHostResize    `json:"resize,omitempty"`        // Get only
	Compose      *string                 `json:"compose,omitempty"`       // Get only: the saved compose file
	EnvKeys      []string                `json:"env_keys,omitempty"`      // Get only: names of the saved env vars
	LogTail      *string                 `json:"log_tail,omitempty"`      // Get only: the installer log of a failed host
}

// ContainerHostCredentials is the first Portainer login. It stops working once the password is
// changed in Portainer.
type ContainerHostCredentials struct {
	PortainerUrl string `json:"portainer_url"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Note         string `json:"note"`
}

// ContainerStats is a container's resource use, as `docker stats` shows it.
type ContainerStats struct {
	Cpu           string `json:"cpu"`            // "0.15%"
	Memory        string `json:"memory"`         // "21.5MiB / 3.8GiB"
	MemoryPercent string `json:"memory_percent"` // "0.55%"
	NetIo         string `json:"net_io"`
	BlockIo       string `json:"block_io"`
}

// Container is one container on a host.
type Container struct {
	Id      string          `json:"id"`
	Name    string          `json:"name"`
	Image   string          `json:"image"`
	State   string          `json:"state"`  // running, exited, restarting, created, paused, dead
	Status  string          `json:"status"` // "Up 2 hours"
	Ports   string          `json:"ports"`
	Service string          `json:"service"` // the compose service
	System  bool            `json:"system"`  // the host's own Traefik or Portainer
	Created string          `json:"created"`
	Stats   *ContainerStats `json:"stats,omitempty"`
}

// ContainerHostDisk is the host's root disk use, in bytes.
type ContainerHostDisk struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
	Free  int64 `json:"free"`
}

// ContainerDomainStatus is a domain with what its DNS answers and its certificate state.
type ContainerDomainStatus struct {
	ContainerDomain
	DNS        []string `json:"dns"`         // the domain's A records now
	PointsHere bool     `json:"points_here"` // one of them is the host's IngressIp
	SSL        string   `json:"ssl"`         // active, waiting_dns or pending
	// NAT hosts: the domain's route on the NAT web proxy (active, pending or missing), why it is not
	// on yet, and the TXT record that also proves the domain is yours.
	Route      *string `json:"route,omitempty"`
	RouteError *string `json:"route_error,omitempty"`
	TxtName    *string `json:"txt_name,omitempty"`
	TxtValue   *string `json:"txt_value,omitempty"`
}

// ContainerHostStatus is read from the host when asked: its containers, disk and certificates.
type ContainerHostStatus struct {
	Containers   []Container             `json:"containers"`
	Disk         *ContainerHostDisk      `json:"disk,omitempty"`
	Certificates []string                `json:"certificates"` // domains with an issued certificate
	Domains      []ContainerDomainStatus `json:"domains"`
}

// CreateContainerHostRequest orders a host.
type CreateContainerHostRequest struct {
	Name string `json:"name"`
	// Plan is a plan slug from Options, e.g. "s-2vcpu-4gb".
	Plan string `json:"plan"`
	// Region ID; nil = the default region.
	Region    *string `json:"region,omitempty"`
	ProjectId *string `json:"project_id,omitempty"` // nil = the team's default project
	// IpType is "public" (the default when empty) or "nat": no public IP, the host's domains are
	// reached through the shared NAT web proxy (ContainerHost.IngressIp). Fixed at creation.
	IpType string `json:"ip_type,omitempty"`
}

// DeployRequest deploys a compose file. A nil Compose deploys the saved one again.
type DeployRequest struct {
	Compose *string `json:"compose,omitempty"`
	Note    *string `json:"note,omitempty"` // up to 200 characters, shown in the deployment list
}

// ContainerEnvResult is the answer to SetEnv: the names now saved, and the deployment it
// started (nil when it did not deploy).
type ContainerEnvResult struct {
	EnvKeys    []string             `json:"env_keys"`
	Deployment *ContainerDeployment `json:"deployment"`
}

// ContainerDomainsResult is the answer to SetDomains: the domains now saved, and the deployment
// it started (nil when it did not deploy).
type ContainerDomainsResult struct {
	Domains    []ContainerDomain    `json:"domains"`
	Deployment *ContainerDeployment `json:"deployment"`
}

// ContainerRegistriesResult is what SetRegistries saved, and the deployment it started (nil when
// deploy was false or no compose file is saved yet).
type ContainerRegistriesResult struct {
	RegistryIds []string             `json:"registry_ids"`
	Deployment  *ContainerDeployment `json:"deployment"`
}

// ListContainerHostsOptions filters List by project.
type ListContainerHostsOptions struct {
	ProjectID string
}

// ListDeploymentsOptions limits ListDeployments (newest first; 0 = the API's default of 50, at most 200).
type ListDeploymentsOptions struct {
	Limit int
}

// ContainerHostsService manages Managed Container Service hosts (/v1/container-hosts): a CC2
// server of the team's own, run for it with Docker, Compose, Traefik (automatic Let's Encrypt
// certificates) and Portainer. The team deploys a docker-compose.yml with env vars and domains.
// Billed per hour from the team's wallet (the server plus the management fee) until deleted.
//
// Creating a host takes about 5 minutes (WaitReady). Live calls (Status, Logs, ContainerAction,
// Deploy, SetEnv / SetDomains with deploy) need a ready host with its server running, else 409.
// A deployment runs on the host in the background: Deploy answers at once with a "running"
// deployment, and WaitDeployment waits for it to finish.
type ContainerHostsService struct{ c *Client }

const containerHostsPath = "/v1/container-hosts"

// Options says whether ordering hosts is switched on and lists the plans with their prices.
func (s *ContainerHostsService) Options(ctx context.Context) (*ContainerHostOptions, error) {
	return getOne[ContainerHostOptions](ctx, s.c, containerHostsPath+"/options")
}

func (s *ContainerHostsService) List(ctx context.Context, opts *ListContainerHostsOptions) ([]ContainerHost, error) {
	q := url.Values{}
	if opts != nil && opts.ProjectID != "" {
		q.Set("project_id", opts.ProjectID)
	}
	return getList[ContainerHost](ctx, s.c, containerHostsPath, q)
}

func (s *ContainerHostsService) Get(ctx context.Context, id string) (*ContainerHost, error) {
	return getOne[ContainerHost](ctx, s.c, containerHostsPath+p(id))
}

// Create orders a host: the server is made and Docker installed on it (about 5 minutes;
// WaitReady waits). The usual wallet and account checks of a CC2 server apply.
func (s *ContainerHostsService) Create(ctx context.Context, req CreateContainerHostRequest, idempotencyKey ...string) (*ContainerHost, error) {
	return send[ContainerHost](ctx, s.c, http.MethodPost, containerHostsPath, req, idempotencyKey)
}

// Delete starts deleting the host and its server (with every container and volume on it);
// WaitDeleted waits until it is gone. Billing stops.
func (s *ContainerHostsService) Delete(ctx context.Context, id string) error {
	return del(ctx, s.c, containerHostsPath+p(id))
}

// Retry runs the installer again on a failed host (it is safe to re-run).
func (s *ContainerHostsService) Retry(ctx context.Context, id string) (*ContainerHost, error) {
	return send[ContainerHost](ctx, s.c, http.MethodPost, containerHostsPath+p(id, "retry"), nil, nil)
}

// ChangePlan moves a ready host to another plan by resizing its server (about a minute of
// downtime; disks only grow, so a plan with a smaller disk is refused). It answers when the
// resize has started; WaitPlan waits until the host is on the new plan.
func (s *ContainerHostsService) ChangePlan(ctx context.Context, id, plan string) (*ContainerHost, error) {
	return send[ContainerHost](ctx, s.c, http.MethodPost, containerHostsPath+p(id, "plan"), map[string]string{"plan": plan}, nil)
}

// Credentials returns the first Portainer login (a secret read). Only for a ready host.
func (s *ContainerHostsService) Credentials(ctx context.Context, id string) (*ContainerHostCredentials, error) {
	return getOne[ContainerHostCredentials](ctx, s.c, containerHostsPath+p(id, "credentials"))
}

// Status reads the host's containers (with CPU and memory), disk use, certificates and the DNS
// state of its domains from the server now.
func (s *ContainerHostsService) Status(ctx context.Context, id string) (*ContainerHostStatus, error) {
	return getOne[ContainerHostStatus](ctx, s.c, containerHostsPath+p(id, "status"))
}

// Logs returns the last lines of a container's log, with timestamps. lines is 10-2000; 0 means
// the API's default (200).
func (s *ContainerHostsService) Logs(ctx context.Context, id, container string, lines int) (string, error) {
	q := url.Values{}
	if lines > 0 {
		q.Set("lines", strconv.Itoa(lines))
	}
	var raw []byte
	if err := s.c.do(ctx, request{method: http.MethodGet, path: containerHostsPath + p(id, "containers", container, "logs"), query: q, raw: &raw}, nil); err != nil {
		return "", err
	}
	return string(raw), nil
}

// ContainerAction starts, stops or restarts a container (ContainerStart, ContainerStop,
// ContainerRestart). Done when it answers.
func (s *ContainerHostsService) ContainerAction(ctx context.Context, id, container, action string) error {
	return s.c.do(ctx, request{method: http.MethodPost, path: containerHostsPath + p(id, "containers", container, "action"),
		body: map[string]string{"action": action}}, nil)
}

// Validate checks a compose file without deploying it and returns its service names.
func (s *ContainerHostsService) Validate(ctx context.Context, id, compose string) ([]string, error) {
	out, err := send[composeServices](ctx, s.c, http.MethodPost, containerHostsPath+p(id, "validate"), DeployRequest{Compose: &compose}, nil)
	if err != nil {
		return nil, err
	}
	return out.Services, nil
}

type composeServices struct {
	Services []string `json:"services"`
}

// Deploy saves the compose file (when given) and starts `docker compose up` on the host with the
// saved env vars and domains. It answers with the running deployment; WaitDeployment waits for
// it. Only one deployment runs at a time (409 while another is running).
func (s *ContainerHostsService) Deploy(ctx context.Context, id string, req DeployRequest) (*ContainerDeployment, error) {
	return send[ContainerDeployment](ctx, s.c, http.MethodPost, containerHostsPath+p(id, "deploy"), req, nil)
}

type containerEnv struct {
	Env map[string]string `json:"env"`
}

// GetEnv returns the saved env vars with their values (a secret read).
func (s *ContainerHostsService) GetEnv(ctx context.Context, id string) (map[string]string, error) {
	out, err := getOne[containerEnv](ctx, s.c, containerHostsPath+p(id, "env"))
	if err != nil {
		return nil, err
	}
	if out.Env == nil {
		return map[string]string{}, nil
	}
	return out.Env, nil
}

// SetEnv replaces every env var with env. With deploy, and a compose file saved, it deploys at
// once (the deployment is in the answer); otherwise the values wait for the next deployment.
func (s *ContainerHostsService) SetEnv(ctx context.Context, id string, env map[string]string, deploy bool) (*ContainerEnvResult, error) {
	if env == nil {
		env = map[string]string{}
	}
	body := struct {
		Env    map[string]string `json:"env"`
		Deploy bool              `json:"deploy"`
	}{env, deploy}
	return send[ContainerEnvResult](ctx, s.c, http.MethodPut, containerHostsPath+p(id, "env"), body, nil)
}

// SetEnvText is SetEnv with NAME=value lines (.env format) instead of a map.
func (s *ContainerHostsService) SetEnvText(ctx context.Context, id, text string, deploy bool) (*ContainerEnvResult, error) {
	body := struct {
		Text   string `json:"text"`
		Deploy bool   `json:"deploy"`
	}{text, deploy}
	return send[ContainerEnvResult](ctx, s.c, http.MethodPut, containerHostsPath+p(id, "env"), body, nil)
}

// SetDomains replaces every domain with domains. With deploy, and a compose file saved, it
// deploys at once; otherwise they wait for the next deployment. The services are checked
// against the compose file when it deploys.
func (s *ContainerHostsService) SetDomains(ctx context.Context, id string, domains []ContainerDomain, deploy bool) (*ContainerDomainsResult, error) {
	if domains == nil {
		domains = []ContainerDomain{}
	}
	body := struct {
		Domains []ContainerDomain `json:"domains"`
		Deploy  bool              `json:"deploy"`
	}{domains, deploy}
	return send[ContainerDomainsResult](ctx, s.c, http.MethodPut, containerHostsPath+p(id, "domains"), body, nil)
}

// SetRegistries chooses the Container Hub registries (by ID, see Registries.List) the host logs in
// to before every `docker compose pull`; empty = none. Docker keeps one login per registry server,
// so at most one registry per server. With deploy, and a compose file saved, it deploys at once.
func (s *ContainerHostsService) SetRegistries(ctx context.Context, id string, registryIDs []string, deploy bool) (*ContainerRegistriesResult, error) {
	if registryIDs == nil {
		registryIDs = []string{}
	}
	body := struct {
		RegistryIds []string `json:"registry_ids"`
		Deploy      bool     `json:"deploy"`
	}{registryIDs, deploy}
	return send[ContainerRegistriesResult](ctx, s.c, http.MethodPut, containerHostsPath+p(id, "registries"), body, nil)
}

// ListDeployments lists a host's deployments, newest first, without their compose files and output.
func (s *ContainerHostsService) ListDeployments(ctx context.Context, id string, opts *ListDeploymentsOptions) ([]ContainerDeployment, error) {
	q := url.Values{}
	if opts != nil && opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	return getList[ContainerDeployment](ctx, s.c, containerHostsPath+p(id, "deployments"), q)
}

// GetDeployment returns one deployment with its compose file and output. A running one is read
// from the host first, so polling this follows its progress.
func (s *ContainerHostsService) GetDeployment(ctx context.Context, id, deploymentID string) (*ContainerDeployment, error) {
	return getOne[ContainerDeployment](ctx, s.c, containerHostsPath+p(id, "deployments", deploymentID))
}

// Redeploy deploys an earlier deployment's compose file and domains again (a rollback); the env
// vars stay as they are now. It answers with the new, running deployment.
func (s *ContainerHostsService) Redeploy(ctx context.Context, id, deploymentID string) (*ContainerDeployment, error) {
	return send[ContainerDeployment](ctx, s.c, http.MethodPost, containerHostsPath+p(id, "deployments", deploymentID, "redeploy"), nil, nil)
}

// WaitReady waits until the host is ready (the server is up and Docker, Traefik and Portainer
// are installed). A failed install returns *ErrStuck with the reason and the end of the log.
func (s *ContainerHostsService) WaitReady(ctx context.Context, id string, opts *WaitOptions) (*ContainerHost, error) {
	var last *ContainerHost
	err := waitFor(ctx, opts, func(ctx context.Context) (bool, error) {
		h, err := s.Get(ctx, id)
		if err != nil {
			return false, err
		}
		last = h
		switch h.Status {
		case ContainerHostReady:
			return true, nil
		case ContainerHostFailed, ContainerHostDeleting, ContainerHostDeleted:
			return false, &ErrStuck{Kind: "container host", ID: id, Status: h.Status, Detail: hostFailure(h)}
		}
		return false, nil
	})
	return last, err
}

// WaitPlan waits until a plan change is applied: the host is on plan and its server is running
// again. A failed resize returns *ErrStuck.
func (s *ContainerHostsService) WaitPlan(ctx context.Context, id, plan string, opts *WaitOptions) (*ContainerHost, error) {
	var last *ContainerHost
	err := waitFor(ctx, opts, func(ctx context.Context) (bool, error) {
		h, err := s.Get(ctx, id)
		if err != nil {
			return false, err
		}
		last = h
		if h.Plan == plan && h.Server.Status == "running" {
			return true, nil
		}
		if h.Resize != nil && h.Resize.Error != nil && *h.Resize.Error != "" && h.Server.Status != "resizing" {
			return false, &ErrStuck{Kind: "container host", ID: id, Status: "resize failed", Detail: *h.Resize.Error}
		}
		if h.Status == ContainerHostDeleting || h.Status == ContainerHostDeleted {
			return false, &ErrStuck{Kind: "container host", ID: id, Status: h.Status}
		}
		return false, nil
	})
	return last, err
}

// WaitDeleted waits until the host is gone.
func (s *ContainerHostsService) WaitDeleted(ctx context.Context, id string, opts *WaitOptions) error {
	return waitFor(ctx, opts, func(ctx context.Context) (bool, error) {
		h, err := s.Get(ctx, id)
		if IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		return h.Status == ContainerHostDeleted, nil
	})
}

// WaitDeployment waits until a deployment has finished. A failed one returns the deployment and
// *ErrStuck with the end of its output.
func (s *ContainerHostsService) WaitDeployment(ctx context.Context, id, deploymentID string, opts *WaitOptions) (*ContainerDeployment, error) {
	var last *ContainerDeployment
	err := waitFor(ctx, opts, func(ctx context.Context) (bool, error) {
		d, err := s.GetDeployment(ctx, id, deploymentID)
		if err != nil {
			return false, err
		}
		last = d
		switch d.Status {
		case DeploymentSucceeded:
			return true, nil
		case DeploymentFailed:
			return false, &ErrStuck{Kind: "deployment", ID: "#" + strconv.Itoa(d.Number), Status: d.Status, Detail: tail(strv(d.Output), 15)}
		}
		return false, nil
	})
	return last, err
}

func hostFailure(h *ContainerHost) string {
	msg := strv(h.Error)
	if t := tail(strv(h.LogTail), 10); t != "" {
		if msg != "" {
			msg += "\n"
		}
		msg += t
	}
	return msg
}

// tail is the last n lines of s.
func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
