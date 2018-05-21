package reconciler

import (
	"testing"

	"context"

	"time"

	"math"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/sky-uk/merlin/reconciler/healthchecks"
	"github.com/sky-uk/merlin/types"
	"github.com/stretchr/testify/mock"
)

func TestReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Reconciler Suite")
}

type servers map[*types.VirtualService][]*types.RealServer

var _ = Describe("Reconciler", func() {

	var (
		service *types.VirtualService
		server  *types.RealServer
	)

	BeforeEach(func() {
		service = &types.VirtualService{
			Id: "svc1",
			Key: &types.VirtualService_Key{
				Ip:       "10.10.10.1",
				Port:     101,
				Protocol: types.Protocol_TCP,
			},
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
				Flags:     []string{"flag-1", "flag-2"},
			},
		}
		server = &types.RealServer{
			ServiceID: service.Id,
			Key: &types.RealServer_Key{
				Ip:   "172.16.1.1",
				Port: 8080,
			},
			Config: &types.RealServer_Config{
				Weight: &wrappers.UInt32Value{Value: 1},
			},
			HealthCheck: &types.RealServer_HealthCheck{
				Endpoint:      &wrappers.StringValue{Value: "http://:102/health"},
				Period:        ptypes.DurationProto(10 * time.Second),
				Timeout:       ptypes.DurationProto(2 * time.Second),
				UpThreshold:   2,
				DownThreshold: 1,
			},
		}
	})

	Describe("Start/Stop", func() {
		It("should add health checks for existing real servers on start", func() {
			storeMock := &storeMock{}
			checkerMock := &checkerMock{}
			r := New(math.MaxInt64, storeMock, nil).(*reconciler)
			r.checker = checkerMock
			server2 := proto.Clone(server).(*types.RealServer)
			server2.Key.Ip = "172.16.1.2"

			storeServices := []*types.VirtualService{service}
			storeMock.On("ListServices", mock.Anything).Return(storeServices, nil)
			storeServers := []*types.RealServer{server, server2}
			storeMock.On("ListServers", mock.Anything, service.Id).Return(storeServers, nil)

			checkerMock.On("SetHealthCheck", service.Id, server.Key, server.HealthCheck,
				mock.AnythingOfType("healthchecks.TransitionFunc")).Return(nil)
			checkerMock.On("SetHealthCheck", service.Id, server2.Key, server2.HealthCheck,
				mock.AnythingOfType("healthchecks.TransitionFunc")).Return(nil)
			checkerMock.On("Stop").Return(nil)

			err := r.Start()
			Expect(err).ToNot(HaveOccurred())
			r.Stop()

			checkerMock.AssertExpectations(GinkgoT())
		})
	})

	Describe("HealthStateWeightUpdater", func() {
		var (
			store *storeMock
			ipvs  *ipvsMock
			r     *reconciler
		)

		BeforeEach(func() {
			store = &storeMock{}
			ipvs = &ipvsMock{}
			r = New(math.MaxInt64, store, ipvs).(*reconciler)
		})

		It("should set the weight to 0 on down transition", func() {
			disabledServer := proto.Clone(server).(*types.RealServer)
			disabledServer.Config.Weight = &wrappers.UInt32Value{Value: 0}
			ipvs.On("UpdateServer", mock.Anything, service.Key, disabledServer).Return(nil)

			fn := r.createHealthStateWeightUpdater(service.Key, server)
			fn(healthchecks.ServerDown)

			ipvs.AssertExpectations(GinkgoT())
		})

		It("should set the weight to original on up transition", func() {
			ipvs.On("UpdateServer", mock.Anything, service.Key, server).Return(nil)

			fn := r.createHealthStateWeightUpdater(service.Key, server)
			fn(healthchecks.ServerUp)

			ipvs.AssertExpectations(GinkgoT())
		})
	})

	Describe("reconcile", func() {
		// Test fixtures for reconcile function.
		// We have to create these from scratch, as the Describe func() is evaluated prior to BeforeEach,
		// so our TableEntries can't refer to data created in a BeforeEach.
		svcKey1 := &types.VirtualService_Key{
			Ip:       "10.10.10.1",
			Port:     101,
			Protocol: types.Protocol_TCP,
		}
		svc1 := &types.VirtualService{
			Id:  "svc1",
			Key: svcKey1,
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
				Flags:     []string{"flag-1", "flag-2"},
			},
		}
		svc1Updated := proto.Clone(svc1).(*types.VirtualService)
		svc1Updated.Config = &types.VirtualService_Config{
			Scheduler: "wrr",
			Flags:     []string{"flag-3"},
		}
		svc1ReorderedFlags := proto.Clone(svc1).(*types.VirtualService)
		svc1ReorderedFlags.Config = &types.VirtualService_Config{
			Scheduler: "sh",
			Flags:     []string{"flag-2", "flag-1"},
		}
		svcKey2 := &types.VirtualService_Key{
			Ip:       "10.10.10.2",
			Port:     102,
			Protocol: types.Protocol_UDP,
		}
		svc2 := &types.VirtualService{
			Key: svcKey2,
			Config: &types.VirtualService_Config{
				Scheduler: "rr",
				Flags:     []string{"flag-1"},
			},
			Id: "svc2",
		}

		serverKey1 := &types.RealServer_Key{
			Ip:   "172.16.1.1",
			Port: 501,
		}
		server1 := &types.RealServer{
			Key: serverKey1,
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 1},
				Forward: types.ForwardMethod_ROUTE,
			},
			HealthCheck: &types.RealServer_HealthCheck{
				Endpoint:      &wrappers.StringValue{Value: "http://:102/health"},
				Period:        ptypes.DurationProto(10 * time.Second),
				Timeout:       ptypes.DurationProto(2 * time.Second),
				UpThreshold:   2,
				DownThreshold: 1,
			},
		}
		server1Updated := proto.Clone(server1).(*types.RealServer)
		server1Updated.Config.Weight.Value = 0
		serverKey2 := &types.RealServer_Key{
			Ip:   "172.16.1.2",
			Port: 502,
		}
		server2 := &types.RealServer{
			Key: serverKey2,
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 1},
				Forward: types.ForwardMethod_ROUTE,
			},
			HealthCheck: &types.RealServer_HealthCheck{
				Endpoint:      &wrappers.StringValue{Value: "http://:304/healme"},
				Period:        ptypes.DurationProto(30 * time.Second),
				Timeout:       ptypes.DurationProto(1 * time.Second),
				UpThreshold:   4,
				DownThreshold: 2,
			},
		}
		disabledServer2 := proto.Clone(server2).(*types.RealServer)
		disabledServer2.Config.Weight.Value = 0

		type reconcileCase struct {
			// services
			actualServices  []*types.VirtualService
			desiredServices []*types.VirtualService
			createdServices []*types.VirtualService
			updatedServices []*types.VirtualService
			deletedServices []*types.VirtualService

			// servers
			actualServers  servers
			desiredServers servers
			createdServers servers
			updatedServers servers
			deletedServers servers

			// health check
			downServers servers
		}

		cases := []TableEntry{
			Entry("add new service", reconcileCase{
				actualServices:  []*types.VirtualService{svc2},
				desiredServices: []*types.VirtualService{svc1, svc2},
				createdServices: []*types.VirtualService{svc1},
			}),
			Entry("update service", reconcileCase{
				actualServices:  []*types.VirtualService{svc1, svc2},
				desiredServices: []*types.VirtualService{svc1Updated, svc2},
				updatedServices: []*types.VirtualService{svc1Updated},
			}),
			Entry("no change in service", reconcileCase{
				actualServices:  []*types.VirtualService{svc1Updated, svc2},
				desiredServices: []*types.VirtualService{svc1Updated, svc2},
			}),
			Entry("delete service", reconcileCase{
				actualServices:  []*types.VirtualService{svc1, svc2},
				desiredServices: []*types.VirtualService{svc1},
				deletedServices: []*types.VirtualService{svc2},
			}),
			Entry("add server", reconcileCase{
				actualServices:  []*types.VirtualService{svc1},
				desiredServices: []*types.VirtualService{svc1},
				actualServers:   servers{svc1: {server2}},
				desiredServers:  servers{svc1: {server1, server2}},
				createdServers:  servers{svc1: {server1}},
			}),
			Entry("update server", reconcileCase{
				actualServices:  []*types.VirtualService{svc1},
				desiredServices: []*types.VirtualService{svc1},
				actualServers:   servers{svc1: {server1, server2}},
				desiredServers:  servers{svc1: {server1Updated, server2}},
				updatedServers:  servers{svc1: {server1Updated}},
			}),
			Entry("delete server", reconcileCase{
				actualServices:  []*types.VirtualService{svc1},
				desiredServices: []*types.VirtualService{svc1},
				actualServers:   servers{svc1: {server1, server2}},
				desiredServers:  servers{svc1: {server2}},
				deletedServers:  servers{svc1: {server1}},
			}),
			Entry("different order of flags causes no update", reconcileCase{
				actualServices:  []*types.VirtualService{svc1},
				desiredServices: []*types.VirtualService{svc1ReorderedFlags},
			}),
			Entry("down server gets a weight of 0", reconcileCase{
				actualServices:  []*types.VirtualService{svc1},
				desiredServices: []*types.VirtualService{svc1},
				actualServers:   servers{svc1: {server1, server2}},
				desiredServers:  servers{svc1: {server1, server2}},
				downServers:     servers{svc1: {server2}},
				updatedServers:  servers{svc1: {disabledServer2}},
			}),
		}

		DescribeTable("reconcile", func(c reconcileCase) {
			storeMock := &storeMock{}
			ipvsMock := &ipvsMock{}
			checkerMock := &checkerMock{}
			r := New(math.MaxInt64, storeMock, ipvsMock).(*reconciler)
			r.checker = checkerMock

			// set defaults
			if c.actualServices == nil {
				c.actualServices = []*types.VirtualService{}
			}
			if c.desiredServices == nil {
				c.desiredServices = []*types.VirtualService{}
			}
			if c.actualServers == nil {
				c.actualServers = make(servers)
			}
			if c.desiredServers == nil {
				c.desiredServers = make(servers)
			}
			if c.downServers == nil {
				c.downServers = make(servers)
			}
			for _, svc := range c.desiredServices {
				if _, ok := c.actualServers[svc]; !ok {
					c.actualServers[svc] = []*types.RealServer{}
				}
				if _, ok := c.desiredServers[svc]; !ok {
					c.desiredServers[svc] = []*types.RealServer{}
				}
				if _, ok := c.downServers[svc]; !ok {
					c.downServers[svc] = []*types.RealServer{}
				}
			}

			// clone services so fixtures aren't shared (store and ipvs structs will be different)
			c.desiredServices = copyServices(c.desiredServices)
			c.actualServices = copyServices(c.actualServices)

			// clone and fill in servers so fixtures aren't shared
			// actual servers are from IPVS, so no serviceID will be set
			c.actualServers = copyServers(c.actualServers, false)
			// deleted servers are from IPVS, so no serviceID will be set
			c.deletedServers = copyServers(c.deletedServers, false)
			c.desiredServers = copyServers(c.desiredServers, true)
			c.createdServers = copyServers(c.createdServers, true)
			c.updatedServers = copyServers(c.updatedServers, true)
			c.downServers = copyServers(c.downServers, true)

			// add expectations
			// store
			storeMock.On("ListServices", mock.Anything).Return(c.desiredServices, nil)
			for service, servers := range c.desiredServers {
				storeMock.On("ListServers", mock.Anything, service.Id).Return(servers, nil)
			}

			// ipvs
			ipvsMock.On("ListServices", mock.Anything).Return(c.actualServices, nil)
			for _, s := range c.createdServices {
				ipvsMock.On("AddService", mock.Anything, s).Return(nil)
			}
			for _, s := range c.updatedServices {
				ipvsMock.On("UpdateService", mock.Anything, s).Return(nil)
			}
			for _, s := range c.deletedServices {
				ipvsMock.On("DeleteService", mock.Anything, s.Key).Return(nil)
			}
			for service, servers := range c.actualServers {
				ipvsMock.On("ListServers", mock.Anything, service.Key).Return(servers, nil)
			}
			for service, servers := range c.createdServers {
				for _, server := range servers {
					ipvsMock.On("AddServer", mock.Anything, service.Key, server).Return(nil)
				}
			}
			for service, servers := range c.updatedServers {
				for _, server := range servers {
					ipvsMock.On("UpdateServer", mock.Anything, service.Key, server).Return(nil)
				}
			}
			for service, servers := range c.deletedServers {
				for _, server := range servers {
					ipvsMock.On("DeleteServer", mock.Anything, service.Key, server).Return(nil)
				}
			}

			// health checker
			for _, servers := range c.desiredServers {
				for _, server := range servers {
					checkerMock.On("SetHealthCheck", server.ServiceID, server.Key, server.HealthCheck,
						mock.AnythingOfType("healthchecks.TransitionFunc")).Return(nil)
				}
			}
			for service, servers := range c.deletedServers {
				for _, server := range servers {
					checkerMock.On("RemHealthCheck", service.Id, server.Key)
				}
			}
			for service, servers := range c.desiredServers {
				for _, server := range servers {
					var isDown bool
					for _, downServer := range c.downServers[service] {
						if proto.Equal(server.Key, downServer.Key) {
							isDown = true
						}
					}
					if isDown {
						checkerMock.On("IsDown", server.ServiceID, server.Key).Return(true)
					} else {
						checkerMock.On("IsDown", server.ServiceID, server.Key).Return(false)
					}
				}
			}

			// reconcile
			r.reconcile()

			// ensure expected outcomes
			storeMock.AssertExpectations(GinkgoT())
			ipvsMock.AssertExpectations(GinkgoT())
			checkerMock.AssertExpectations(GinkgoT())
		},
			cases...)
	})
})

func copyServices(services []*types.VirtualService) []*types.VirtualService {
	var copiedServices []*types.VirtualService
	for _, service := range services {
		copiedServices = append(copiedServices, proto.Clone(service).(*types.VirtualService))
	}
	return copiedServices
}

func copyServers(serviceToServers servers, setServiceID bool) servers {
	copiedServers := make(servers)
	for service, realServers := range serviceToServers {
		var copied []*types.RealServer
		for _, realServer := range realServers {
			copiedServer := proto.Clone(realServer).(*types.RealServer)
			if setServiceID {
				// will have a ServiceID as desired servers come from the store
				copiedServer.ServiceID = service.Id
			}
			copied = append(copied, copiedServer)
		}
		copiedServers[service] = copied
	}
	return copiedServers
}

type storeMock struct {
	mock.Mock
}

func (s *storeMock) Close() {
	panic("not implemented")
}
func (s *storeMock) ListServices(ctx context.Context) ([]*types.VirtualService, error) {
	args := s.Called(ctx)
	return args.Get(0).([]*types.VirtualService), args.Error(1)
}
func (s *storeMock) ListServers(ctx context.Context, serviceID string) ([]*types.RealServer, error) {
	args := s.Called(ctx, serviceID)
	return args.Get(0).([]*types.RealServer), args.Error(1)
}

type ipvsMock struct {
	mock.Mock
}

func (i *ipvsMock) Close() {
	i.Called()
}

func (i *ipvsMock) AddService(ctx context.Context, svc *types.VirtualService) error {
	args := i.Called(ctx, svc)
	return args.Error(0)
}
func (i *ipvsMock) UpdateService(ctx context.Context, svc *types.VirtualService) error {
	args := i.Called(ctx, svc)
	return args.Error(0)
}
func (i *ipvsMock) DeleteService(ctx context.Context, key *types.VirtualService_Key) error {
	args := i.Called(ctx, key)
	return args.Error(0)
}
func (i *ipvsMock) ListServices(ctx context.Context) ([]*types.VirtualService, error) {
	args := i.Called(ctx)
	return args.Get(0).([]*types.VirtualService), args.Error(1)
}
func (i *ipvsMock) AddServer(ctx context.Context, key *types.VirtualService_Key, server *types.RealServer) error {
	args := i.Called(ctx, key, server)
	return args.Error(0)
}
func (i *ipvsMock) UpdateServer(ctx context.Context, key *types.VirtualService_Key, server *types.RealServer) error {
	args := i.Called(ctx, key, server)
	return args.Error(0)
}
func (i *ipvsMock) DeleteServer(ctx context.Context, key *types.VirtualService_Key, server *types.RealServer) error {
	args := i.Called(ctx, key, server)
	return args.Error(0)
}
func (i *ipvsMock) ListServers(ctx context.Context, key *types.VirtualService_Key) ([]*types.RealServer, error) {
	args := i.Called(ctx, key)
	return args.Get(0).([]*types.RealServer), args.Error(1)
}

type checkerMock struct {
	mock.Mock
}

func (m *checkerMock) IsDown(serviceID string, key *types.RealServer_Key) bool {
	args := m.Called(serviceID, key)
	return args.Bool(0)
}

func (m *checkerMock) SetHealthCheck(serviceID string, key *types.RealServer_Key, check *types.RealServer_HealthCheck,
	transitionFunc healthchecks.TransitionFunc) error {
	args := m.Called(serviceID, key, check, transitionFunc)
	return args.Error(0)
}

func (m *checkerMock) RemHealthCheck(serviceID string, key *types.RealServer_Key) {
	m.Called(serviceID, key)
}

func (m *checkerMock) Stop() {
	m.Called()
}
