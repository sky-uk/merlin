package store

import (
	"context"
	"time"

	"encoding/base64"
	"fmt"
	"strings"

	"github.com/cenkalti/backoff"
	"github.com/coreos/etcd/client"
	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
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
	GetServer(ctx context.Context, serviceID string, key *types.RealServer_Key) (*types.RealServer, error)
	PutServer(ctx context.Context, server *types.RealServer) error
	DeleteServer(ctx context.Context, serviceID string, key *types.RealServer_Key) error
	ListServices(context.Context) ([]*types.VirtualService, error)
	ListServers(ctx context.Context, serviceID string) ([]*types.RealServer, error)
	// Subscribe to changes. subscriber is called whenever a change occurs in the store.
	Subscribe(subscriber func(), stopCh <-chan struct{})
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
	log.Debug("Creating etcd2 client")
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

func unmarshal(pb proto.Message, raw string) proto.Message {
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		panic(fmt.Errorf("unable to decode - did you break backwards compatibility?: %v", err))
	}
	if err := proto.Unmarshal(b, pb); err != nil {
		panic(fmt.Errorf("unable to unmarshal - did you break backwards compatibility?: %v", err))
	}
	return pb
}

func unmarshalService(raw string) *types.VirtualService {
	var service types.VirtualService
	return unmarshal(&service, raw).(*types.VirtualService)
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
		panic(err)
	}

	enc := base64.StdEncoding.EncodeToString(b)
	if _, err := s.kapi.Set(ctx, s.serviceKey(service.Id), enc, nil); err != nil {
		return fmt.Errorf("unable to store service %s: %v", service.Id, err)
	}

	return nil
}

func (s *store) DeleteService(ctx context.Context, serviceID string) error {
	_, err := s.kapi.Delete(ctx, s.serviceKey(serviceID), nil)
	return err
}

func (s *store) serverDir(serviceID string) string {
	return s.prefix + servers + "/" + serviceID
}

func (s *store) serverKey(serviceID string, key *types.RealServer_Key) string {
	return fmt.Sprintf("%s/%s:%d", s.serverDir(serviceID), key.Ip, key.Port)
}

func unmarshalServer(raw string) *types.RealServer {
	var server types.RealServer
	return unmarshal(&server, raw).(*types.RealServer)
}

func (s *store) GetServer(ctx context.Context, serviceID string, key *types.RealServer_Key) (*types.RealServer, error) {
	if key == nil {
		// can't retrieve server without a key
		return nil, nil
	}
	resp, err := s.kapi.Get(ctx, s.serverKey(serviceID, key), getOpts)
	if client.IsKeyNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve server from store: %v", err)
	}
	server := unmarshalServer(resp.Node.Value)
	return server, nil
}

func (s *store) PutServer(ctx context.Context, server *types.RealServer) error {
	if err := s.initDir(s.serverDir(server.ServiceID)); err != nil {
		return fmt.Errorf("unable to init %s/%s: %v", servers, server.ServiceID, err)
	}

	b, err := proto.Marshal(server)
	if err != nil {
		panic(err)
	}

	enc := base64.StdEncoding.EncodeToString(b)
	key := s.serverKey(server.ServiceID, server.Key)
	if _, err := s.kapi.Set(ctx, key, enc, nil); err != nil {
		return fmt.Errorf("unable to store server %s: %v", key, err)
	}

	return nil
}

func (s *store) DeleteServer(ctx context.Context, serviceID string, key *types.RealServer_Key) error {
	serverKey := s.serverKey(serviceID, key)
	_, err := s.kapi.Delete(ctx, serverKey, nil)
	return err
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
	resp, err := s.kapi.Get(ctx, s.serverDir(serviceID), getOpts)
	if client.IsKeyNotFound(err) {
		return []*types.RealServer{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to list servers for %s: %v", serviceID, err)
	}

	var servers []*types.RealServer
	for _, node := range resp.Node.Nodes {
		server := unmarshalServer(node.Value)
		servers = append(servers, server)
	}
	return servers, nil
}

func (s *store) Subscribe(subscriber func(), stopCh <-chan struct{}) {
	options := &client.WatcherOptions{
		Recursive: true,
	}
	watcher := s.kapi.Watcher(s.prefix, options)
	respCh := make(chan *client.Response)

	go func() {
		ctx, cancelFunc := context.WithCancel(context.Background())
		defer cancelFunc()
		handleWatcherUpdates(ctx, watcher, respCh)

		for {
			select {
			case <-respCh:
				subscriber()
			case <-stopCh:
				return
			}
		}
	}()
}

func handleWatcherUpdates(ctx context.Context, watcher client.Watcher, respCh chan<- *client.Response) {
	handler := func() error {
		resp, err := watcher.Next(ctx)
		if err == nil {
			respCh <- resp
		} else {
			if ctx.Err() != nil {
				// context was cancelled, exit handler
				return &backoff.PermanentError{Err: err}
			}
			log.Warnf("etcd watcher: %v", err)
		}
		return err
	}

	expBackoff := backoff.NewExponentialBackOff()
	// never stop retrying
	expBackoff.MaxElapsedTime = 0

	go func() {
		for {
			err := backoff.Retry(handler, expBackoff)
			if err != nil {
				break
			}
		}
	}()
}
