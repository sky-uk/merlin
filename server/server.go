package server

import (
	"context"

	"fmt"

	"net"

	"math"

	log "github.com/Sirupsen/logrus"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes/empty"
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/sky-uk/merlin/reconciler"
	"github.com/sky-uk/merlin/store"
	"github.com/sky-uk/merlin/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/url"
)

type server struct {
	store      store.Store
	reconciler reconciler.Reconciler
}

// New merlin server implementation.
func New(store store.Store, reconciler reconciler.Reconciler) types.MerlinServer {
	return &server{
		store:      store,
		reconciler: reconciler,
	}
}

var (
	emptyResponse = &empty.Empty{}
)

func validateService(service *types.VirtualService) error {
	if len(service.Id) == 0 {
		return status.Error(codes.InvalidArgument, "service id required")
	}
	if service.Key == nil {
		return status.Error(codes.InvalidArgument, "service ip:port:protocol key required")
	}
	if len(service.Key.Ip) == 0 {
		return status.Error(codes.InvalidArgument, "service IP required")
	}
	if net.ParseIP(service.Key.Ip) == nil {
		return status.Error(codes.InvalidArgument, "unable to parse service IP")
	}
	if service.Key.Port == 0 {
		return status.Error(codes.InvalidArgument, "service port required")
	}
	if service.Key.Port > math.MaxUint16 {
		return status.Errorf(codes.InvalidArgument, "invalid port %d", service.Key.Port)
	}
	if service.Key.Protocol == 0 {
		return status.Error(codes.InvalidArgument, "service protocol required")
	}
	if _, ok := types.Protocol_name[int32(service.Key.Protocol)]; !ok {
		return status.Errorf(codes.InvalidArgument, "unrecognized protocol %d", service.Key.Protocol)
	}
	if service.Config == nil {
		return status.Error(codes.InvalidArgument, "service config required")
	}
	if service.Config.Scheduler == "" {
		return status.Error(codes.InvalidArgument, "service scheduler required")
	}
	if service.HealthCheck != nil {
		u, err := url.Parse(service.HealthCheck.Endpoint)
		if err != nil {
			return status.Errorf(codes.InvalidArgument, "health check endpoint %q must be a valid url: %v",
				service.HealthCheck.Endpoint, err)
		}
		switch u.Scheme {
		case "http":
			// valid
		default:
			return status.Errorf(codes.InvalidArgument, "health check endpoint scheme %q not recognized",
				u.Scheme)
		}
		if u.Port() == "" {
			return status.Errorf(codes.InvalidArgument, "health check endpoint is missing port")
		}
	}
	return nil
}

func (s *server) CreateService(ctx context.Context, service *types.VirtualService) (*empty.Empty, error) {
	if err := validateService(service); err != nil {
		return emptyResponse, err
	}

	prev, err := s.store.GetService(ctx, service.Id)
	if err != nil {
		return emptyResponse, fmt.Errorf("failed to check service exists: %v", err)
	}
	if prev != nil {
		return emptyResponse, status.Errorf(codes.AlreadyExists, "service %s already exists", service.Id)
	}

	if err := s.store.PutService(ctx, service); err != nil {
		return emptyResponse, fmt.Errorf("failed to create service: %v", err)
	}

	s.reconciler.Sync()

	log.Infof("Created %v", service)
	return emptyResponse, nil
}

func (s *server) UpdateService(ctx context.Context, update *types.VirtualService) (*empty.Empty, error) {
	prev, err := s.store.GetService(ctx, update.Id)
	if err != nil {
		return emptyResponse, fmt.Errorf("failed to check server exists: %v", err)
	}
	if prev == nil {
		return emptyResponse, status.Errorf(codes.NotFound, "service %s doesn't exist", update.Id)
	}

	next := proto.Clone(prev).(*types.VirtualService)
	// clear flags so they are replaced
	if len(update.Config.Flags) > 0 {
		next.Config.Flags = nil
	}
	proto.Merge(next.Config, update.Config)
	if next.HealthCheck == nil {
		next.HealthCheck = update.HealthCheck
	} else {
		proto.Merge(next.HealthCheck, update.HealthCheck)
	}

	if proto.Equal(prev, next) {
		log.Infof("No update of %s", update.Id)
		return emptyResponse, nil
	}

	if err := validateService(next); err != nil {
		return emptyResponse, err
	}

	if err := s.store.PutService(ctx, next); err != nil {
		return emptyResponse, fmt.Errorf("failed to update service: %v", err)
	}

	s.reconciler.Sync()

	log.Infof("Updated %v", next)
	return emptyResponse, nil
}

func (s *server) DeleteService(ctx context.Context, wrappedID *wrappers.StringValue) (*empty.Empty, error) {
	id := wrappedID.GetValue()
	if err := s.store.DeleteService(ctx, id); err != nil {
		return emptyResponse, fmt.Errorf("failed to delete service %s: %v", id, err)
	}
	s.reconciler.Sync()
	log.Infof("Deleted %s", id)
	return emptyResponse, nil
}

func validateServer(server *types.RealServer) error {
	if len(server.ServiceID) == 0 {
		return status.Error(codes.InvalidArgument, "service ID required")
	}
	if server.Key == nil {
		return status.Error(codes.InvalidArgument, "server IP:port required")
	}
	if len(server.Key.Ip) == 0 {
		return status.Error(codes.InvalidArgument, "server IP required")
	}
	if net.ParseIP(server.Key.Ip) == nil {
		return status.Errorf(codes.InvalidArgument, "unable to parse server IP %s", server.Key.Ip)
	}
	if server.Key.Port == 0 {
		return status.Error(codes.InvalidArgument, "server port required")
	}
	if server.Key.Port > math.MaxUint16 {
		return status.Errorf(codes.InvalidArgument, "invalid port %d", server.Key.Port)
	}
	if server.Config == nil {
		return status.Error(codes.InvalidArgument, "server config required")
	}
	if server.Config.Forward == types.ForwardMethod_UNSET_FORWARD_METHOD {
		return status.Error(codes.InvalidArgument, "server forward method required")
	}
	if server.Config.Weight == nil {
		return status.Error(codes.InvalidArgument, "server weight required")
	}
	return nil
}

func (s *server) CreateServer(ctx context.Context, server *types.RealServer) (*empty.Empty, error) {
	if err := validateServer(server); err != nil {
		return emptyResponse, err
	}

	svc, err := s.store.GetService(ctx, server.ServiceID)
	if err != nil {
		return emptyResponse, fmt.Errorf("failed to check service %s exists: %v", server.ServiceID, err)
	}
	if svc == nil {
		return emptyResponse, status.Errorf(codes.NotFound, "service does not exist, can't create server %v", server)
	}

	prev, err := s.store.GetServer(ctx, server.ServiceID, server.Key)
	if err != nil {
		return emptyResponse, fmt.Errorf("failed to check server %v exists: %v", server, err)
	}
	if prev != nil {
		return emptyResponse, status.Errorf(codes.AlreadyExists, "server %v already exists", server)
	}

	if err := s.store.PutServer(ctx, server); err != nil {
		return emptyResponse, fmt.Errorf("failed to create server: %v", err)
	}

	s.reconciler.Sync()

	log.Infof("Created %v", server)
	return emptyResponse, nil
}

func (s *server) UpdateServer(ctx context.Context, update *types.RealServer) (*empty.Empty, error) {
	prev, err := s.store.GetServer(ctx, update.ServiceID, update.Key)
	if err != nil {
		return emptyResponse, fmt.Errorf("failed to check server exists: %v", err)
	}
	if prev == nil {
		return emptyResponse, status.Errorf(codes.NotFound, "server %s/%s doesn't exist",
			update.ServiceID, update.Key)
	}

	next := proto.Clone(prev).(*types.RealServer)
	proto.Merge(next.Config, update.Config)

	if proto.Equal(prev, next) {
		log.Infof("No update of %s/%s", update.ServiceID, update.Key)
		return emptyResponse, nil
	}

	if err := validateServer(next); err != nil {
		return emptyResponse, err
	}

	if err := s.store.PutServer(ctx, next); err != nil {
		return emptyResponse, fmt.Errorf("failed to update server: %v", err)
	}

	s.reconciler.Sync()

	log.Infof("Updated %v", next)
	return emptyResponse, nil
}

func (s *server) DeleteServer(ctx context.Context, server *types.RealServer) (*empty.Empty, error) {
	if err := s.store.DeleteServer(ctx, server.ServiceID, server.Key); err != nil {
		return emptyResponse, fmt.Errorf("failed to delete server %s: %v", server, err)
	}
	s.reconciler.Sync()
	log.Infof("Deleted %s/%s", server.ServiceID, server.Key)
	return emptyResponse, nil
}

func (s *server) List(ctx context.Context, _ *empty.Empty) (*types.ListResponse, error) {
	svcs, err := s.store.ListServices(ctx)
	if err != nil {
		return nil, err
	}

	var resp types.ListResponse
	for _, svc := range svcs {
		servers, err := s.store.ListServers(ctx, svc.Id)
		if err != nil {
			return nil, err
		}
		resp.Items = append(resp.Items, &types.ListResponse_Item{Service: svc, Servers: servers})
	}
	return &resp, nil
}
