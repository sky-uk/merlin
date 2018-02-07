package store

import (
	"context"
	"time"

	"encoding/base64"
	"fmt"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/coreos/etcd/client"
	"github.com/gogo/protobuf/proto"
	"github.com/sky-uk/merlin/types"
)

const (
	services = "/services"
	servers  = "/servers"
)

var (
	getOpts = &client.GetOptions{Quorum: true}
)

// Store for saving desired IPVS state.
type Store interface {
	GetService(ctx context.Context, serviceID string) (*types.VirtualService, error)
	PutService(context.Context, *types.VirtualService) error
	DeleteService(ctx context.Context, serviceID string) error
	PutServer(*types.RealServer) error
	DeleteServer(*types.RealServer) error
	ListServices(context.Context) ([]*types.VirtualService, error)
	ListServers(ctx context.Context, serviceID string) ([]*types.RealServer, error)
}

type store struct {
	c      client.Client
	prefix string
	kapi   client.KeysAPI
}

// NewEtcd2 returns a Store implementation using an etcd2 backing store.
func NewEtcd2(endpoints []string, prefix string) (Store, error) {
	cfg := client.Config{
		Endpoints:               endpoints,
		Transport:               client.DefaultTransport,
		HeaderTimeoutPerRequest: time.Second,
	}
	log.Debug("creating etcd2 client")
	c, err := client.New(cfg)
	if err != nil {
		return nil, err
	}
	s := &store{c: c, prefix: prefix, kapi: client.NewKeysAPI(c)}

	return s, s.init()
}

func (s *store) init() error {
	if !strings.HasPrefix(s.prefix, "/") {
		s.prefix = "/" + s.prefix
	}

	// initialize prefix directory
	if err := s.initDir(s.prefix); err != nil {
		return fmt.Errorf("failed to create %s directory: %v", s.prefix, err)
	}

	// initialize services directory
	if err := s.initDir(s.prefix + services); err != nil {
		return fmt.Errorf("failed to create %s%s directory: %v", s.prefix, services, err)
	}

	// initialize servers directory
	if err := s.initDir(s.prefix + servers); err != nil {
		return fmt.Errorf("failed to create %s%s directory: %v", s.prefix, servers, err)
	}

	return nil
}

func (s *store) initDir(dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	opts := client.SetOptions{Dir: true}

	_, err := s.kapi.Get(ctx, dir, nil)
	if !client.IsKeyNotFound(err) {
		return err
	}

	if _, err := s.kapi.Set(ctx, dir, "", &opts); err != nil {
		return err
	}
	return nil
}

func (s *store) serviceKey(id string) string {
	return s.prefix + services + "/" + id
}

func unmarshalService(raw string) *types.VirtualService {
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		panic(fmt.Errorf("unable to decode - did you break backwards compatibility?: %v", err))
	}
	var service types.VirtualService
	if err := proto.Unmarshal(b, &service); err != nil {
		panic(fmt.Errorf("unable to unmarshal - did you break backwards compatibility?: %v", err))
	}
	return &service
}

func (s *store) GetService(ctx context.Context, serviceID string) (*types.VirtualService, error) {
	resp, err := s.kapi.Get(ctx, s.serviceKey(serviceID), getOpts)
	if client.IsKeyNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve service from store: %v", err)
	}
	svc := unmarshalService(resp.Node.Value)
	return svc, nil
}

func (s *store) PutService(ctx context.Context, service *types.VirtualService) error {
	b, err := proto.Marshal(service)
	if err != nil {
		return err
	}

	enc := base64.StdEncoding.EncodeToString(b)
	if _, err := s.kapi.Set(ctx, s.serviceKey(service.Id), enc, nil); err != nil {
		return err
	}

	return nil
}

func (s *store) DeleteService(ctx context.Context, serviceID string) error {
	_, err := s.kapi.Delete(ctx, s.serviceKey(serviceID), nil)
	return err
}

func (s *store) PutServer(*types.RealServer) error {
	panic("implement me")
}

func (s *store) DeleteServer(*types.RealServer) error {
	panic("implement me")
}

func (s *store) ListServices(ctx context.Context) ([]*types.VirtualService, error) {
	resp, err := s.kapi.Get(ctx, s.serviceKey(""), getOpts)
	if err != nil {
		return nil, fmt.Errorf("unable to list services: %v", err)
	}

	var services []*types.VirtualService
	for _, node := range resp.Node.Nodes {
		service := unmarshalService(node.Value)
		services = append(services, service)
	}
	return services, nil
}

func (s *store) ListServers(ctx context.Context, serviceID string) ([]*types.RealServer, error) {
	return nil, nil
}
