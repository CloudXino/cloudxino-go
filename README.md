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
`SSHKeys`, `ReservedIPs` and `Account` (who the key acts for).

### Reserved IPs

A reserved IP is a public IPv4 address the team keeps until it deletes it, pointed at a CC2 server
in the same region. Moving it to another server keeps the address.

```go
ip, err := client.ReservedIPs.Create(ctx, cloudxino.CreateReservedIPRequest{Region: &region})
ip, err = client.ReservedIPs.Assign(ctx, ip.Id, server.Id) // or move it
ip, err = client.ReservedIPs.Unassign(ctx, ip.Id)
err = client.ReservedIPs.Delete(ctx, ip.Id)                 // unassigns first
```

When no address is free (or reserved IPs are off for the API), `Create` and `Assign` return a 503
with code `unavailable`; check it with `cloudxino.IsUnavailable(err)`. It is not retried.

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
`Servers.WaitDeleted`, `LoadBalancers.WaitActive` and `LoadBalancers.WaitDeleted` poll until the
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
