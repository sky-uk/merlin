// Package ipvs encapsulates the details of the ipvs netlink library.
package ipvs

import (
	"syscall"

	"fmt"

	"net"

	"context"

	"github.com/docker/libnetwork/ipvs"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/sky-uk/merlin/types"
)

// IPVS shim.
type IPVS interface {
	Close()
	AddService(ctx context.Context, svc *types.VirtualService) error
	UpdateService(ctx context.Context, svc *types.VirtualService) error
	DeleteService(ctx context.Context, key *types.VirtualService_Key) error
	ListServices(ctx context.Context) ([]*types.VirtualService, error)
	AddServer(ctx context.Context, key *types.VirtualService_Key, server *types.RealServer) error
	UpdateServer(ctx context.Context, key *types.VirtualService_Key, server *types.RealServer) error
	DeleteServer(ctx context.Context, key *types.VirtualService_Key, server *types.RealServer) error
	ListServers(ctx context.Context, key *types.VirtualService_Key) ([]*types.RealServer, error)
}

// ipvsHandle for libnetwork/ipvs.
type ipvsHandle interface {
	Close()
	GetServices() ([]*ipvs.Service, error)
	NewService(*ipvs.Service) error
	UpdateService(*ipvs.Service) error
	DelService(*ipvs.Service) error
	GetDestinations(*ipvs.Service) ([]*ipvs.Destination, error)
	NewDestination(*ipvs.Service, *ipvs.Destination) error
	UpdateDestination(*ipvs.Service, *ipvs.Destination) error
	DelDestination(*ipvs.Service, *ipvs.Destination) error
}

type shim struct {
	handle ipvsHandle
}

// New IPVS shim. This creates an underlying netlink socket. Call Close() to release the associated resources.
func New() (IPVS, error) {
	h, err := ipvs.New("")
	if err != nil {
		return nil, fmt.Errorf("unable to init ipvs: %v", err)
	}
	return &shim{
		handle: h,
	}, nil
}

func (s *shim) Close() {
	s.handle.Close()
}

func createHandleServiceKey(key *types.VirtualService_Key) (*ipvs.Service, error) {
	protNum, err := toProtocolBits(key.Protocol)
	if err != nil {
		return nil, err
	}
	svc := &ipvs.Service{
		Address:       net.ParseIP(key.Ip),
		Protocol:      protNum,
		Port:          uint16(key.Port),
		AddressFamily: syscall.AF_INET,
	}
	return svc, nil
}

func createHandleService(svc *types.VirtualService) (*ipvs.Service, error) {
	ipvsSvc, err := createHandleServiceKey(svc.Key)
	if err != nil {
		return nil, err
	}
	ipvsSvc.SchedName = svc.Config.Scheduler
	ipvsSvc.Flags = toFlagBits(svc.Config.Flags)
	return ipvsSvc, nil
}

func (s *shim) AddService(ctx context.Context, svc *types.VirtualService) error {
	ipvsSvc, err := createHandleService(svc)
	if err != nil {
		return err
	}

	c := make(chan error, 1)
	go func() { c <- s.handle.NewService(ipvsSvc) }()
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for netlink NewService, possible goroutine leak (%v)", ctx.Err())
	case err := <-c:
		return err
	}
}

func (s *shim) UpdateService(ctx context.Context, svc *types.VirtualService) error {
	ipvsSvc, err := createHandleService(svc)
	if err != nil {
		return err
	}

	c := make(chan error, 1)
	go func() { c <- s.handle.UpdateService(ipvsSvc) }()
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for netlink UpdateService, possible goroutine leak (%v)", ctx.Err())
	case err := <-c:
		return err
	}
}

func (s *shim) DeleteService(ctx context.Context, key *types.VirtualService_Key) error {
	ipvsSvc, err := createHandleServiceKey(key)
	if err != nil {
		return err
	}

	c := make(chan error, 1)
	go func() { c <- s.handle.DelService(ipvsSvc) }()
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for netlink DelService, possible goroutine leak (%v)", ctx.Err())
	case err := <-c:
		return err
	}
}

func (s *shim) ListServices(ctx context.Context) ([]*types.VirtualService, error) {
	c := make(chan struct {
		services []*ipvs.Service
		error
	}, 1)
	go func() {
		svcs, err := s.handle.GetServices()
		c <- struct {
			services []*ipvs.Service
			error
		}{svcs, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout waiting for netlink ListServices, possible goroutine leak (%v)", ctx.Err())
	case r := <-c:
		if r.error != nil {
			return nil, fmt.Errorf("failed to list services: %v", r.error)
		}

		var svcs []*types.VirtualService
		for _, hSvc := range r.services {
			protocol, err := fromProtocolBits(hSvc.Protocol)
			if err != nil {
				return nil, err
			}
			svc := &types.VirtualService{
				Key: &types.VirtualService_Key{
					Ip:       hSvc.Address.String(),
					Port:     uint32(hSvc.Port),
					Protocol: protocol,
				},
				Config: &types.VirtualService_Config{
					Scheduler: hSvc.SchedName,
					Flags:     fromFlagBits(hSvc.Flags),
				},
			}
			svcs = append(svcs, svc)
		}
		return svcs, nil
	}
}

func createHandleDestination(server *types.RealServer, full bool) (*ipvs.Destination, error) {
	dest := &ipvs.Destination{
		Address:       net.ParseIP(server.Key.Ip),
		Port:          uint16(server.Key.Port),
		AddressFamily: syscall.AF_INET,
	}
	if !full {
		return dest, nil
	}
	if server.Config.Forward != types.ForwardMethod_UNSET_FORWARD_METHOD {
		fwdbits, ok := forwardingMethods[server.Config.Forward]
		if !ok {
			return nil, fmt.Errorf("invalid forwarding method %q", server.Config.Forward)
		}
		dest.ConnectionFlags = fwdbits
	}
	if server.Config.Weight != nil {
		dest.Weight = int(server.Config.Weight.Value)
	}
	return dest, nil
}

func createHandleServiceKeyAndDestination(key *types.VirtualService_Key, server *types.RealServer,
	fullServer bool) (*ipvs.Service, *ipvs.Destination, error) {

	svc, err := createHandleServiceKey(key)
	if err != nil {
		return nil, nil, err
	}
	dest, err := createHandleDestination(server, fullServer)
	if err != nil {
		return nil, nil, err
	}
	return svc, dest, nil
}

func (s *shim) AddServer(ctx context.Context, key *types.VirtualService_Key, server *types.RealServer) error {
	svc, dest, err := createHandleServiceKeyAndDestination(key, server, true)
	if err != nil {
		return err
	}

	c := make(chan error, 1)
	go func() { c <- s.handle.NewDestination(svc, dest) }()
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for netlink NewDestination, possible goroutine leak (%v)", ctx.Err())
	case err := <-c:
		return err
	}
}

func (s *shim) UpdateServer(ctx context.Context, key *types.VirtualService_Key, server *types.RealServer) error {
	svc, dest, err := createHandleServiceKeyAndDestination(key, server, true)
	if err != nil {
		return err
	}

	c := make(chan error, 1)
	go func() { c <- s.handle.UpdateDestination(svc, dest) }()
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for netlink UpdateDestination, possible goroutine leak (%v)", ctx.Err())
	case err := <-c:
		return err
	}
}

func (s *shim) DeleteServer(ctx context.Context, key *types.VirtualService_Key, server *types.RealServer) error {
	svc, dest, err := createHandleServiceKeyAndDestination(key, server, false)
	if err != nil {
		return err
	}

	c := make(chan error, 1)
	go func() { c <- s.handle.DelDestination(svc, dest) }()
	select {
	case <-ctx.Done():
		return fmt.Errorf("timeout waiting for netlink DelDestination, possible goroutine leak (%v)", ctx.Err())
	case err := <-c:
		return err
	}
}

func (s *shim) ListServers(ctx context.Context, key *types.VirtualService_Key) ([]*types.RealServer, error) {
	svc, err := createHandleServiceKey(key)
	if err != nil {
		return nil, err
	}

	c := make(chan struct {
		destinations []*ipvs.Destination
		error
	}, 1)
	go func() {
		dests, err := s.handle.GetDestinations(svc)
		c <- struct {
			destinations []*ipvs.Destination
			error
		}{dests, err}
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout waiting for netlink ListDestinations, possible goroutine leak (%v)", ctx.Err())
	case r := <-c:
		if r.error != nil {
			return nil, r.error
		}

		var servers []*types.RealServer
		for _, dest := range r.destinations {
			fwdBits := dest.ConnectionFlags & ipvs.ConnectionFlagFwdMask
			fwd, ok := forwardingMethodsInverted[fwdBits]
			if !ok {
				return nil, fmt.Errorf("unable to list backends, unexpected forward method bits %#x", fwdBits)
			}
			server := &types.RealServer{
				Key: &types.RealServer_Key{
					Ip:   dest.Address.String(),
					Port: uint32(dest.Port),
				},
				Config: &types.RealServer_Config{
					Weight:  &wrappers.UInt32Value{Value: uint32(dest.Weight)},
					Forward: fwd,
				},
			}
			servers = append(servers, server)
		}

		return servers, nil
	}
}
