package store

import (
	"context"

	"fmt"
	"strings"

	"github.com/cenkalti/backoff"
	"github.com/coreos/etcd/client"
	"github.com/coreos/etcd/clientv3"
	"github.com/golang/protobuf/proto"
	log "github.com/sirupsen/logrus"
	"github.com/sky-uk/merlin/types"
)

type etcd3store struct {
	client *clientv3.Client
	prefix string
}

// NewEtcd3 returns a Store implementation using an etcd3 backing store.
func NewEtcd3(endpoints []string, prefix string) (Store, error) {
	cfg := clientv3.Config{
		Endpoints: endpoints,
	}
	log.Debug("Creating etcd3 client")
	c, err := clientv3.New(cfg)
	if err != nil {
		return nil, err
	}
	s := &etcd3store{client: c, prefix: prefix}

	return s, s.init()
}

func (s *etcd3store) init() error {
	if !strings.HasPrefix(s.prefix, "/") {
		s.prefix = "/" + s.prefix
	}

	return nil
}

func (s *etcd3store) serviceKey(id string) string {
	return s.prefix + services + "/" + id
}

func (s *etcd3store) GetService(ctx context.Context, serviceID string) (*types.VirtualService, error) {

	resp, err := s.client.Get(ctx, s.serviceKey(serviceID))
	if len(resp.Kvs) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve service from store: %v", err)
	}
	svc := unmarshalService(resp.Kvs[0].Value)
	return svc, nil
}

func (s *etcd3store) PutService(ctx context.Context, service *types.VirtualService) error {
	b, err := proto.Marshal(service)
	if err != nil {
		panic(err)
	}

	if _, err := s.client.Put(ctx, s.serviceKey(service.Id), string(b)); err != nil {
		return fmt.Errorf("unable to store service %s: %v", service.Id, err)
	}

	return nil
}

func (s *etcd3store) DeleteService(ctx context.Context, serviceID string) error {
	_, err := s.client.Delete(ctx, s.serviceKey(serviceID))
	return err
}

func (s *etcd3store) serverDir(serviceID string) string {
	return s.prefix + servers + "/" + serviceID
}

func (s *etcd3store) serverKey(serviceID string, key *types.RealServer_Key) string {
	return fmt.Sprintf("%s/%s:%d", s.serverDir(serviceID), key.Ip, key.Port)
}

func (s *etcd3store) GetServer(ctx context.Context, serviceID string, key *types.RealServer_Key) (*types.RealServer, error) {
	if key == nil {
		// can't retrieve server without a key
		return nil, nil
	}
	resp, err := s.client.Get(ctx, s.serverKey(serviceID, key))
	if len(resp.Kvs) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve server from store: %v", err)
	}
	server := unmarshalServer(resp.Kvs[0].Value)
	return server, nil
}

func (s *etcd3store) PutServer(ctx context.Context, server *types.RealServer) error {
	b, err := proto.Marshal(server)
	if err != nil {
		panic(err)
	}

	key := s.serverKey(server.ServiceID, server.Key)
	if _, err := s.client.Put(ctx, key, string(b)); err != nil {
		return fmt.Errorf("unable to store server %s: %v", key, err)
	}

	return nil
}

func (s *etcd3store) DeleteServer(ctx context.Context, serviceID string, key *types.RealServer_Key) error {
	serverKey := s.serverKey(serviceID, key)
	_, err := s.client.Delete(ctx, serverKey)
	return err
}

func (s *etcd3store) ListServices(ctx context.Context) ([]*types.VirtualService, error) {
	resp, err := s.client.Get(ctx, s.serviceKey(""), clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("unable to list services: %v", err)
	}

	var services []*types.VirtualService
	for _, node := range resp.Kvs {
		service := unmarshalService(node.Value)
		services = append(services, service)
	}
	return services, nil
}

func (s *etcd3store) ListServers(ctx context.Context, serviceID string) ([]*types.RealServer, error) {
	resp, err := s.client.Get(ctx, s.serverDir(serviceID), clientv3.WithPrefix())
	if client.IsKeyNotFound(err) {
		return []*types.RealServer{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to list servers for %s: %v", serviceID, err)
	}

	var servers []*types.RealServer
	for _, node := range resp.Kvs {
		server := unmarshalServer(node.Value)
		servers = append(servers, server)
	}
	return servers, nil
}

func (s *etcd3store) Subscribe(subscriber func(), stopCh <-chan struct{}) {

	go func() {
		ctx, cancelFunc := context.WithCancel(context.Background())

		watcher := s.client.Watch(ctx, s.prefix, clientv3.WithPrefix())
		respCh := make(chan clientv3.WatchResponse)

		defer cancelFunc()
		s.handleWatcherUpdates(ctx, watcher, respCh)

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

func (s *etcd3store) handleWatcherUpdates(ctx context.Context, watcher clientv3.WatchChan, respCh chan<- clientv3.WatchResponse) {
	handler := func() error {
		resp := <-watcher
		if resp.Err() == nil {
			respCh <- resp
		} else {
			if ctx.Err() != nil {
				// context was cancelled, exit handler
				return &backoff.PermanentError{Err: resp.Err()}
			}
			log.Warnf("etcd watcher: %v", resp.Err())
		}
		return resp.Err()
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
