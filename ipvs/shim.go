// Package ipvs encapsulates the details of the ipvs netlink library.
package ipvs

import (
	"syscall"

	"fmt"

	"net"

	"sort"

	log "github.com/Sirupsen/logrus"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/mqliang/libipvs"
	"github.com/sky-uk/merlin/types"
)

var (
	schedulerFlags = map[string]uint32{
		"flag-1": libipvs.IP_VS_SVC_F_SCHED1,
		"flag-2": libipvs.IP_VS_SVC_F_SCHED2,
		"flag-3": libipvs.IP_VS_SVC_F_SCHED3,
	}
	schedulerFlagsInverted map[uint32]string

	forwardingMethods = map[types.ForwardMethod]libipvs.FwdMethod{
		types.ForwardMethod_ROUTE:  libipvs.IP_VS_CONN_F_DROUTE,
		types.ForwardMethod_MASQ:   libipvs.IP_VS_CONN_F_MASQ,
		types.ForwardMethod_TUNNEL: libipvs.IP_VS_CONN_F_TUNNEL,
	}
	forwardingMethodsInverted map[libipvs.FwdMethod]types.ForwardMethod
)

func init() {
	schedulerFlagsInverted = make(map[uint32]string)
	for k, v := range schedulerFlags {
		schedulerFlagsInverted[v] = k
	}
	forwardingMethodsInverted = make(map[libipvs.FwdMethod]types.ForwardMethod)
	for k, v := range forwardingMethods {
		forwardingMethodsInverted[v] = k
	}
}

// IPVS shim.
type IPVS interface {
	AddService(svc *types.VirtualService) error
	UpdateService(svc *types.VirtualService) error
	DeleteService(key *types.VirtualService_Key) error
	ListServices() ([]*types.VirtualService, error)
	AddServer(key *types.VirtualService_Key, server *types.RealServer) error
	UpdateServer(key *types.VirtualService_Key, server *types.RealServer) error
	DeleteServer(key *types.VirtualService_Key, server *types.RealServer) error
	ListServers(key *types.VirtualService_Key) ([]*types.RealServer, error)
}

type shim struct {
	handle libipvs.IPVSHandle
}

// New IPVS shim.
func New(handle libipvs.IPVSHandle) IPVS {
	return &shim{
		handle: handle,
	}
}

func toIPVSProtocol(protocol types.Protocol) (libipvs.Protocol, error) {
	switch protocol {
	case types.Protocol_TCP:
		return libipvs.Protocol(syscall.IPPROTO_TCP), nil
	case types.Protocol_UDP:
		return libipvs.Protocol(syscall.IPPROTO_UDP), nil
	default:
		return 0, fmt.Errorf("unknown protocol %q", protocol)
	}
}

func fromIPVSProtocol(protocol libipvs.Protocol) (types.Protocol, error) {
	switch protocol {
	case syscall.IPPROTO_TCP:
		return types.Protocol_TCP, nil
	case syscall.IPPROTO_UDP:
		return types.Protocol_UDP, nil
	default:
		return 0, fmt.Errorf("unknown protocol %q", protocol)
	}
}

func initIPVSService(key *types.VirtualService_Key) (*libipvs.Service, error) {
	protNum, err := toIPVSProtocol(key.Protocol)
	if err != nil {
		return nil, err
	}
	svc := &libipvs.Service{
		Address:       net.ParseIP(key.Ip),
		Protocol:      libipvs.Protocol(protNum),
		Port:          uint16(key.Port),
		AddressFamily: syscall.AF_INET,
	}
	return svc, nil
}

func createFlagbits(flags []string) (libipvs.Flags, error) {
	var flagbits uint32
	for _, flag := range flags {
		if b, exists := schedulerFlags[flag]; exists {
			flagbits |= b
		} else {
			log.Warnf("unknown scheduler flag %q, ignoring", flag)
		}
	}
	r := libipvs.Flags{
		Flags: flagbits,
		// set all bits to 1
		Mask: ^uint32(0),
	}
	return r, nil
}

func (s *shim) AddService(svc *types.VirtualService) error {
	ipvsSvc, err := initIPVSService(svc.Key)
	if err != nil {
		return err
	}
	ipvsSvc.SchedName = svc.Config.Scheduler
	ipvsSvc.Flags, err = createFlagbits(svc.Config.Flags)
	if err != nil {
		return err
	}
	return s.handle.NewService(ipvsSvc)
}

func (s *shim) UpdateService(svc *types.VirtualService) error {
	ipvsSvc, err := initIPVSService(svc.Key)
	if err != nil {
		return err
	}
	ipvsSvc.SchedName = svc.Config.Scheduler
	ipvsSvc.Flags, err = createFlagbits(svc.Config.Flags)
	if err != nil {
		return err
	}
	return s.handle.UpdateService(ipvsSvc)
}

func (s *shim) DeleteService(key *types.VirtualService_Key) error {
	svc, err := initIPVSService(key)
	if err != nil {
		return err
	}
	return s.handle.DelService(svc)
}

func convertFlagbits(flagbits uint32) []string {
	var flags []string
	for f, v := range schedulerFlagsInverted {
		if flagbits&f != 0 {
			flags = append(flags, v)
		}
	}
	sort.Strings(flags)
	return flags
}

func (s *shim) ListServices() ([]*types.VirtualService, error) {
	ipvsSvcs, err := s.handle.ListServices()
	if err != nil {
		return nil, err
	}

	var svcs []*types.VirtualService
	for _, isvc := range ipvsSvcs {
		prot, err := fromIPVSProtocol(isvc.Protocol)
		if err != nil {
			return nil, err
		}
		svc := &types.VirtualService{
			Key: &types.VirtualService_Key{
				Ip:       isvc.Address.String(),
				Port:     uint32(isvc.Port),
				Protocol: prot,
			},
			Config: &types.VirtualService_Config{
				Scheduler: isvc.SchedName,
				Flags:     convertFlagbits(isvc.Flags.Flags),
			},
		}
		svcs = append(svcs, svc)
	}

	return svcs, nil
}

func createDest(server *types.RealServer, full bool) (*libipvs.Destination, error) {
	dest := &libipvs.Destination{
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
		dest.FwdMethod = libipvs.FwdMethod(fwdbits)
	}
	if server.Config.Weight != nil {
		dest.Weight = server.Config.Weight.Value
	}
	return dest, nil
}

func (s *shim) AddServer(key *types.VirtualService_Key, server *types.RealServer) error {
	svc, err := initIPVSService(key)
	if err != nil {
		return err
	}
	dest, err := createDest(server, true)
	if err != nil {
		return err
	}
	return s.handle.NewDestination(svc, dest)
}

func (s *shim) UpdateServer(key *types.VirtualService_Key, server *types.RealServer) error {
	svc, err := initIPVSService(key)
	if err != nil {
		return err
	}
	dest, err := createDest(server, true)
	if err != nil {
		return err
	}
	return s.handle.UpdateDestination(svc, dest)
}

func (s *shim) DeleteServer(key *types.VirtualService_Key, server *types.RealServer) error {
	svc, err := initIPVSService(key)
	if err != nil {
		return err
	}
	dest, err := createDest(server, false)
	if err != nil {
		return err
	}
	return s.handle.DelDestination(svc, dest)
}

func (s *shim) ListServers(key *types.VirtualService_Key) ([]*types.RealServer, error) {
	svc, err := initIPVSService(key)
	if err != nil {
		return nil, err
	}

	dests, err := s.handle.ListDestinations(svc)
	if err != nil {
		return nil, err
	}

	var servers []*types.RealServer
	for _, dest := range dests {
		fwd, ok := forwardingMethodsInverted[dest.FwdMethod]
		if !ok {
			return nil, fmt.Errorf("unable to list backends, unexpected forward method %#x", dest.FwdMethod)
		}
		server := &types.RealServer{
			Key: &types.RealServer_Key{
				Ip:   dest.Address.String(),
				Port: uint32(dest.Port),
			},
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: dest.Weight},
				Forward: fwd,
			},
		}
		servers = append(servers, server)
	}

	return servers, nil
}
