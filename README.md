# cloudxino-go

The Go client for the [CloudXino](https://cloudxino.com) cloud API. The
[Terraform provider](https://github.com/CloudXino/terraform-provider-cloudxino) and the
[`cxctl`](https://github.com/CloudXino/cxctl) CLI are both built on it.

```sh
go get github.com/cloudxino/cloudxino-go
```

## Usage

Create an API key in the console (**Account → API keys**). The key acts with the role you give it
(Admin, Developer, Billing or Read-only) in the team you created it in.

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cloudxino/cloudxino-go"
)

func main() {
	ctx := context.Background()
	client, err := cloudxino.NewClient(os.Getenv("CLOUDXINO_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}

	key, err := client.SSHKeys.Create(ctx, "laptop", "ssh-ed25519 AAAA... me@laptop")
	if err != nil {
		log.Fatal(err)
	}

	server, err := client.Servers.Create(ctx, cloudxino.CreateMC2Request{
		Name:      "web-01",
		Image:     "debian-13",
		Size:      "s-1vcpu-1gb",
		SshKeyIds: &[]string{key.Id},
	})
	if err != nil {
		log.Fatal(err)
	}
	server, err = client.Servers.WaitReady(ctx, server.Id, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(*server.Name, *server.IpAddress)
}
```

Services: `Servers` (CC2), `Firewalls`, `LoadBalancers`, `DNS` (zones and records), `Projects`,
`SSHKeys`, `ReservedIPs`, `Clusters` (CKS, managed Kubernetes), `ContainerHosts` (Managed Container
Service) and `Account` (who the key acts for).

### Reserved IPs

A reserved IP is a public IPv4 address the team keeps until it deletes it, pointed at a CC2 server,
a CC2 load balancer or a CKS LoadBalancer Service in the same region. Moving it keeps the address.

```go
ip, err := client.ReservedIPs.Create(ctx, cloudxino.CreateReservedIPRequest{Region: &region})
ip, err = client.ReservedIPs.Assign(ctx, ip.Id, server.Id) // or move it
ip, err = client.ReservedIPs.AssignLoadBalancer(ctx, ip.Id, lb.Id)
ip, err = client.ReservedIPs.AssignCKSLoadBalancer(ctx, ip.Id, svcLB.Id) // the cluster switches within a minute
ip, err = client.ReservedIPs.Unassign(ctx, ip.Id)
err = client.ReservedIPs.Delete(ctx, ip.Id)                 // unassigns first
```

When no address is free (or reserved IPs are off for the API), `Create` and `Assign` return a 503
with code `unavailable`; check it with `cloudxino.IsUnavailable(err)`. It is not retried.

### Kubernetes clusters (CKS)

```go
cl, err := client.Clusters.Create(ctx, cloudxino.CreateClusterRequest{
	Name:     "prod",
	NodeSize: "k8s-2vcpu-4gb", // GET /v1/pricing → cluster.node_sizes
})
cl, err = client.Clusters.WaitActive(ctx, cl.Id, &cloudxino.WaitOptions{Timeout: 30 * time.Minute})
kubeconfig, err := client.Clusters.Kubeconfig(ctx, cl.Id) // admin kubeconfig (YAML), only while active
cl, err = client.Clusters.Update(ctx, cl.Id, cloudxino.UpdateClusterRequest{Name: &newName})
pool, err := client.Clusters.CreateNodePool(ctx, cl.Id, cloudxino.CreateNodePoolRequest{Name: "big", NodeSize: "k8s-4vcpu-8gb"})
pool, err = client.Clusters.UpdateNodePool(ctx, cl.Id, pool.Id, cloudxino.UpdateNodePoolRequest{Count: &four})
err = client.Clusters.Delete(ctx, cl.Id)
err = client.Clusters.WaitDeleted(ctx, cl.Id, nil)
```

A cluster can be read at most once per 15 seconds per user; the cluster waiters poll at that pace
(`cloudxino.ClusterPollInterval`) and the client waits out a `429` by its `Retry-After`.

The cluster and node pool types are written by hand in `clusters.go`: the API does not publish a
schema for these answers.

### Managed containers (MCS)

A container host is a CC2 server of the team's own with Docker, Compose, Traefik (automatic Let's
Encrypt certificates) and Portainer. Deploy a `docker-compose.yml` to it with env vars and domains.

```go
opts, err := client.ContainerHosts.Options(ctx) // opts.Enabled, opts.Plans (slugs and monthly prices)
h, err := client.ContainerHosts.Create(ctx, cloudxino.CreateContainerHostRequest{Name: "apps", Plan: "s-2vcpu-4gb"})
h, err = client.ContainerHosts.WaitReady(ctx, h.Id, &cloudxino.WaitOptions{Timeout: 30 * time.Minute})

// save env vars and domains first (deploy=false), then make one deployment with all of it
_, err = client.ContainerHosts.SetEnv(ctx, h.Id, map[string]string{"DB_PASSWORD": pw}, false)
_, err = client.ContainerHosts.SetDomains(ctx, h.Id, []cloudxino.ContainerDomain{{Domain: "app.example.com", Service: "web", Port: 8080}}, false)
dep, err := client.ContainerHosts.Deploy(ctx, h.Id, cloudxino.DeployRequest{Compose: &compose})
dep, err = client.ContainerHosts.WaitDeployment(ctx, h.Id, dep.Id, nil) // a failure is *ErrStuck with the end of the output

st, err := client.ContainerHosts.Status(ctx, h.Id) // containers, disk, certificates, domain DNS
logs, err := client.ContainerHosts.Logs(ctx, h.Id, "app-web-1", 200)
err = client.ContainerHosts.ContainerAction(ctx, h.Id, "app-web-1", cloudxino.ContainerRestart)
h, err = client.ContainerHosts.ChangePlan(ctx, h.Id, "s-4vcpu-8gb") // resizes the server (about a minute of downtime)
h, err = client.ContainerHosts.WaitPlan(ctx, h.Id, "s-4vcpu-8gb", nil)
dep, err = client.ContainerHosts.Redeploy(ctx, h.Id, oldDeploymentID) // roll back to an earlier compose file
err = client.ContainerHosts.Delete(ctx, h.Id)
```

`SetEnv` and `SetDomains` replace the whole set. With `deploy` set to true, and a compose file
already saved, they deploy at once and return that deployment. Live calls need a ready host whose
server is running; otherwise they answer 409. Only one deployment runs at a time.

**NAT hosts.** `IpType: cloudxino.ContainerHostIpNat` orders a host without a public IP (cheaper; see
`opts.IpTypes` and each plan's `NatPerMonth`). Its domains reach it on 80/443 through the shared NAT
web proxy: point their A records at `h.IngressIp` (for any host, that is the IP to use in DNS), at most
`h.Nat.MaxDomains` of them. `Status` shows each domain's proxy `Route`. The type is fixed at creation.

The container host types are written by hand in `containerhosts.go`, like the cluster types.

### Retries and idempotency

- Requests are retried on `429`, maintenance (`503`, but not `503 unavailable`) and gateway errors, waiting as long as the API's
  `Retry-After` says, otherwise with exponential backoff (`WithRetries` to tune it).
- Every `POST` carries an `Idempotency-Key`, and retries reuse it, so a retried create never makes a
  second server or charges twice. Pass your own key (the last argument of `Create`) to stay safe
  across process restarts too.

### Errors

Errors from the API are `*cloudxino.APIError` with a stable `Code` (`not_found`,
`insufficient_balance`, `rate_limited`, `validation_error`, ...). Use `cloudxino.IsNotFound(err)`,
`cloudxino.IsConflict(err)`, `cloudxino.IsUnavailable(err)` or `cloudxino.ErrorCode(err)` to branch on them.

### Waiting

Creating, resizing and deleting take a while. `Servers.WaitReady`, `Servers.WaitResized`,
`Servers.WaitDeleted`, `LoadBalancers.WaitActive`, `LoadBalancers.WaitDeleted`, `Clusters.WaitActive`, `Clusters.WaitDeleted`,
`ContainerHosts.WaitReady`, `ContainerHosts.WaitPlan`, `ContainerHosts.WaitDeleted` and
`ContainerHosts.WaitDeployment` poll until the
change is done and return `*cloudxino.ErrStuck` when the resource ends in an error state.

## Development

The models in `types.gen.go` are generated from `openapi/cloudxino.json`, a subset of the API's
OpenAPI document:

```sh
curl -s https://cloudxino.com/api/openapi.json > /tmp/full.json
python3 scripts/filter_openapi.py /tmp/full.json > openapi/cloudxino.json
go generate ./...
go test ./...
```

## License

Apache License 2.0, see [LICENSE](LICENSE).
