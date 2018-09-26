package store

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/sky-uk/merlin/types"
)

const (
	services = "/services"
	servers  = "/servers"
)

// Store for saving desired IPVS state.
type Store interface {
	GetService(ctx context.Context, serviceID string) (*types.VirtualService, error)
	PutService(context.Context, *types.VirtualService) error
	DeleteService(ctx context.Context, serviceID string) error
	GetServer(ctx context.Context, serviceID string, key *types.RealServer_Key) (*types.RealServer, error)
	PutServer(ctx context.Context, server *types.RealServer) error
	DeleteServer(ctx context.Context, serviceID string, key *types.RealServer_Key) error
	ListServices(context.Context) ([]*types.VirtualService, error)
	ListServers(ctx context.Context, serviceID string) ([]*types.RealServer, error)
	// Subscribe to changes. subscriber is called whenever a change occurs in the store.
	Subscribe(subscriber func(), stopCh <-chan struct{})
}

// NewStore returns a Store implementation based upon the storeBackend parameter
func NewStore(storeBackend string, endpoints []string, prefix string) (Store, error) {

	switch storeBackend {
	case "etcd2":
		return NewEtcd2(endpoints, prefix)
	case "etcd3":
		return NewEtcd3(endpoints, prefix)
	default:
		return nil, fmt.Errorf("unknown store backend: %s", storeBackend)
	}
}

func base64decode(raw string) []byte {
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		panic(fmt.Errorf("unable to decode - did you break backwards compatibility?: %v", err))
	}
	return b
}

func unmarshal(pb proto.Message, b []byte) proto.Message {
	if err := proto.Unmarshal(b, pb); err != nil {
		panic(fmt.Errorf("unable to unmarshal - did you break backwards compatibility?: %v", err))
	}
	return pb
}

func unmarshalService(raw []byte) *types.VirtualService {
	var service types.VirtualService
	return unmarshal(&service, raw).(*types.VirtualService)
}

func unmarshalServer(raw []byte) *types.RealServer {
	var server types.RealServer
	return unmarshal(&server, raw).(*types.RealServer)
}
