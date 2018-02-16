_Note_: This project is under development and not ready yet for use.

# merlin

Merlin is a distributed IPVS manager. It can be used to create a clustered load balancer. It provides:
* An API and command line tool to administer configuration.
* Replication of IPVS configuration across multiple nodes.
* Health checks of attached servers.

Requirements:
* etcd cluster for state
* IPVS kernel modules

Usage:

```bash
# run on every IPVS node
merlin -store-endpoints http://etcd0:2379,http://etcd1:2379,http://etcd3:2379
```

Administer:

```bash
# merlinhost is any IPVS node running merlin
meradm -H merlinhost list
meradm -H merlinhost service add mylb tcp 10.1.1.1:80 -s sh -b flag-1,flag-2
meradm -h # display other commands
```

Library:

```go
import "context"
import "fmt"
import "google.golang.org/grpc"
import "github.com/sky-uk/merlin/types"

func main() {
	conn, _ := grpc.Dial("merlinhost:4282", grpc.WithInsecure())
	defer conn.Close()
	c := types.NewMerlinClient(conn)
	resp, _ := c.List(context.Background(), &empty.Empty{})
	for _, item := range resp.Items {
		fmt.Println(item)
	}
}
```

# Design

The desired state is stored in an etcd cluster.

Every participating merlin node will watch etcd for changes, and then update their local ipvs to match the
desired state.

merlin was originally inspired by [gorb](https://github.com/kobolog/gorb). The main differences are:

* IPVS is reconciled against actual state, improving robustness.
* cli tool that closely mimics `ipvsadm`, with gRPC for API usage. 
* More test coverage, including end to end tests to verify behavior.

# Development

You need golang 1.9+.

```bash
make setup          # setup required tools and dependencies
make install check  # run tests and install
make                # same as `install check`
make format         # format all files
make proto          # regenerate protobuf/gRPC bindings. requires protoc installed.
```

Dependencies are managed with [dep](https://golang.github.io/dep). `vendor` folder is never checked in.

gRPC definitions are managed in [types](types/) and are generated with `make proto`.
Ensure you run `make setup` so you have the latest golang generator.

Testing should be done with [gingko](http://onsi.github.io/ginkgo/)/[gomega](http://onsi.github.io/gomega/).
[e2e](e2e/) tests cover the CRUD interaction, unit tests cover asynchronous interactions such as IPVS reconciliation.

# Remaining features

* etcd3 with watchers
* bgp client for ECMP
