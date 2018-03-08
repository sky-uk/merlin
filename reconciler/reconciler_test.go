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
	"github.com/sky-uk/merlin/types"
	"github.com/stretchr/testify/mock"
)

func TestReconciler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Reconciler Suite")
}

var _ = Describe("Reconciler", func() {
	// test fixtures
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
		HealthCheck: &types.VirtualService_HealthCheck{
			Endpoint:      &wrappers.StringValue{Value: "http://:102/health"},
			Period:        ptypes.DurationProto(10 * time.Second),
			Timeout:       ptypes.DurationProto(2 * time.Second),
			UpThreshold:   2,
			DownThreshold: 1,
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
	}
	server1Updated := &types.RealServer{
		Key: serverKey1,
		Config: &types.RealServer_Config{
			Weight:  &wrappers.UInt32Value{Value: 0},
			Forward: types.ForwardMethod_ROUTE,
		},
	}
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
	}
	disabledServer2 := proto.Clone(server2).(*types.RealServer)
	disabledServer2.Config.Weight = &wrappers.UInt32Value{Value: 0}

	Describe("reconcile", func() {
		type servers map[*types.VirtualService][]*types.RealServer
		// map of serviceID to list of IPs
		type serverIPs map[string][]string

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
			downIPs serverIPs
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
				downIPs:         serverIPs{svc1.Id: {server2.Key.Ip}},
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
			if c.downIPs == nil {
				c.downIPs = make(serverIPs)
			}
			for _, svc := range c.desiredServices {
				if _, ok := c.actualServers[svc]; !ok {
					c.actualServers[svc] = []*types.RealServer{}
				}
				if _, ok := c.desiredServers[svc]; !ok {
					c.desiredServers[svc] = []*types.RealServer{}
				}
				if _, ok := c.downIPs[svc.Id]; !ok {
					c.downIPs[svc.Id] = []string{}
				}
			}

			// add expectations
			// store
			// clone services to ensure reconciler gets different instances from the store versus ipvs
			var storeServices []*types.VirtualService
			for _, service := range c.desiredServices {
				storeServices = append(storeServices, proto.Clone(service).(*types.VirtualService))
			}
			storeMock.On("ListServices", mock.Anything).Return(storeServices, nil)
			for k, v := range c.desiredServers {
				// clone servers to ensure reconciler gets different instances from the store versus ipvs
				var storeServers []*types.RealServer
				for _, server := range v {
					storeServers = append(storeServers, proto.Clone(server).(*types.RealServer))
				}
				storeMock.On("ListServers", mock.Anything, k.Id).Return(storeServers, nil)
			}

			// ipvs
			ipvsMock.On("ListServices").Return(c.actualServices, nil)
			for _, s := range c.createdServices {
				ipvsMock.On("AddService", s).Return(nil)
			}
			for _, s := range c.updatedServices {
				ipvsMock.On("UpdateService", s).Return(nil)
			}
			for _, s := range c.deletedServices {
				ipvsMock.On("DeleteService", s.Key).Return(nil)
			}
			for k, v := range c.actualServers {
				ipvsMock.On("ListServers", k.Key).Return(v, nil)
			}
			for k, v := range c.createdServers {
				for _, server := range v {
					ipvsMock.On("AddServer", k.Key, server).Return(nil)
				}
			}
			for k, v := range c.updatedServers {
				for _, server := range v {
					ipvsMock.On("UpdateServer", k.Key, server).Return(nil)
				}
			}
			for k, v := range c.deletedServers {
				for _, server := range v {
					ipvsMock.On("DeleteServer", k.Key, server).Return(nil)
				}
			}

			// health checker
			for _, service := range c.desiredServices {
				checkerMock.On("SetHealthCheck", service.Id, service.HealthCheck).Return(nil)
			}
			for _, service := range c.deletedServices {
				checkerMock.On("SetHealthCheck", service.Id, &types.VirtualService_HealthCheck{}).Return(nil)
			}
			for service, server := range c.createdServers {
				for _, server := range server {
					checkerMock.On("AddServer", service.Id, server.Key.Ip)
				}
			}
			for service, server := range c.deletedServers {
				for _, server := range server {
					checkerMock.On("RemServer", service.Id, server.Key.Ip)
				}
			}
			for serviceID, ips := range c.downIPs {
				checkerMock.On("GetDownServers", serviceID).Return(ips)
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

	Describe("Start/Stop", func() {
		It("should add health checks for existing real servers on start", func() {
			storeMock := &storeMock{}
			checkerMock := &checkerMock{}
			r := New(math.MaxInt64, storeMock, nil).(*reconciler)
			r.checker = checkerMock

			storeServices := []*types.VirtualService{svc1}
			storeMock.On("ListServices", mock.Anything).Return(storeServices, nil)
			storeServers := []*types.RealServer{server1, server2}
			storeMock.On("ListServers", mock.Anything, svc1.Id).Return(storeServers, nil)

			checkerMock.On("SetHealthCheck", svc1.Id, svc1.HealthCheck).Return(nil)
			checkerMock.On("AddServer", svc1.Id, server1.Key.Ip)
			checkerMock.On("AddServer", svc1.Id, server2.Key.Ip)
			checkerMock.On("Stop").Return(nil)

			err := r.Start()
			Expect(err).ToNot(HaveOccurred())
			r.Stop()

			checkerMock.AssertExpectations(GinkgoT())
		})
	})
})

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

func (i *ipvsMock) AddService(svc *types.VirtualService) error {
	args := i.Called(svc)
	return args.Error(0)
}
func (i *ipvsMock) UpdateService(svc *types.VirtualService) error {
	args := i.Called(svc)
	return args.Error(0)
}
func (i *ipvsMock) DeleteService(key *types.VirtualService_Key) error {
	args := i.Called(key)
	return args.Error(0)
}
func (i *ipvsMock) ListServices() ([]*types.VirtualService, error) {
	args := i.Called()
	return args.Get(0).([]*types.VirtualService), args.Error(1)
}
func (i *ipvsMock) AddServer(key *types.VirtualService_Key, server *types.RealServer) error {
	args := i.Called(key, server)
	return args.Error(0)
}
func (i *ipvsMock) UpdateServer(key *types.VirtualService_Key, server *types.RealServer) error {
	args := i.Called(key, server)
	return args.Error(0)
}
func (i *ipvsMock) DeleteServer(key *types.VirtualService_Key, server *types.RealServer) error {
	args := i.Called(key, server)
	return args.Error(0)
}
func (i *ipvsMock) ListServers(key *types.VirtualService_Key) ([]*types.RealServer, error) {
	args := i.Called(key)
	return args.Get(0).([]*types.RealServer), args.Error(1)
}

type checkerMock struct {
	mock.Mock
}

func (m *checkerMock) GetDownServers(id string) []string {
	args := m.Called(id)
	return args.Get(0).([]string)
}

func (m *checkerMock) SetHealthCheck(id string, check *types.VirtualService_HealthCheck) error {
	args := m.Called(id, check)
	return args.Error(0)
}

func (m *checkerMock) AddServer(id string, server string) {
	m.Called(id, server)
}

func (m *checkerMock) RemServer(id string, server string) {
	m.Called(id, server)
}

func (m *checkerMock) Stop() {
	m.Called()
}
