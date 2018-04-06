// Package ipvs encapsulates the details of the ipvs netlink library.
package ipvs

import (
	"syscall"

	"fmt"

	"net"

	"github.com/docker/libnetwork/ipvs"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/sky-uk/merlin/types"
)

// IPVS shim.
type IPVS interface {
	Close()
	AddService(svc *types.VirtualService) error
	UpdateService(svc *types.VirtualService) error
	DeleteService(key *types.VirtualService_Key) error
	ListServices() ([]*types.VirtualService, error)
	AddServer(key *types.VirtualService_Key, server *types.RealServer) error
	UpdateServer(key *types.VirtualService_Key, server *types.RealServer) error
	DeleteServer(key *types.VirtualService_Key, server *types.RealServer) error
	ListServers(key *types.VirtualService_Key) ([]*types.RealServer, error)
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
	h, err := ipvs.New("/")
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

func createHandleService(key *types.VirtualService_Key) (*ipvs.Service, error) {
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

func (s *shim) AddService(svc *types.VirtualService) error {
	ipvsSvc, err := createHandleService(svc.Key)
	if err != nil {
		return err
	}
	ipvsSvc.SchedName = svc.Config.Scheduler
	ipvsSvc.Flags = toFlagBits(svc.Config.Flags)
	if err != nil {
		return err
	}
	return s.handle.NewService(ipvsSvc)
}

func (s *shim) UpdateService(svc *types.VirtualService) error {
	ipvsSvc, err := createHandleService(svc.Key)
	if err != nil {
		return err
	}
	ipvsSvc.SchedName = svc.Config.Scheduler
	ipvsSvc.Flags = toFlagBits(svc.Config.Flags)
	if err != nil {
		return err
	}
	return s.handle.UpdateService(ipvsSvc)
}

func (s *shim) DeleteService(key *types.VirtualService_Key) error {
	svc, err := createHandleService(key)
	if err != nil {
		return err
	}
	return s.handle.DelService(svc)
}

func (s *shim) ListServices() ([]*types.VirtualService, error) {
	hServices, err := s.handle.GetServices()
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %v", err)
	}

	var svcs []*types.VirtualService
	for _, hSvc := range hServices {
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

func (s *shim) AddServer(key *types.VirtualService_Key, server *types.RealServer) error {
	svc, err := createHandleService(key)
	if err != nil {
		return err
	}
	dest, err := createHandleDestination(server, true)
	if err != nil {
		return err
	}
	return s.handle.NewDestination(svc, dest)
}

func (s *shim) UpdateServer(key *types.VirtualService_Key, server *types.RealServer) error {
	svc, err := createHandleService(key)
	if err != nil {
		return err
	}
	dest, err := createHandleDestination(server, true)
	if err != nil {
		return err
	}
	return s.handle.UpdateDestination(svc, dest)
}

func (s *shim) DeleteServer(key *types.VirtualService_Key, server *types.RealServer) error {
	svc, err := createHandleService(key)
	if err != nil {
		return err
	}
	dest, err := createHandleDestination(server, false)
	if err != nil {
		return err
	}
	return s.handle.DelDestination(svc, dest)
}

func (s *shim) ListServers(key *types.VirtualService_Key) ([]*types.RealServer, error) {
	svc, err := createHandleService(key)
	if err != nil {
		return nil, err
	}

	dests, err := s.handle.GetDestinations(svc)
	if err != nil {
		return nil, err
	}

	var servers []*types.RealServer
	for _, dest := range dests {
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
