package reconciler

import (
	"testing"

	"context"

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
	svcKey1 := &types.VirtualService_Key{
		Ip:       "10.10.10.1",
		Port:     101,
		Protocol: types.Protocol_TCP,
	}
	svc1 := &types.VirtualService{
		Key: svcKey1,
		Config: &types.VirtualService_Config{
			Scheduler: "sh",
			Flags:     []string{"flag-1", "flag-2"},
		},
		Id: "svc1",
	}
	svc1Updated := &types.VirtualService{
		Key: svcKey1,
		Config: &types.VirtualService_Config{
			Scheduler: "wrr",
			Flags:     []string{"flag-3"},
		},
		Id: "svc1",
	}
	svc1ReorderedFlags := &types.VirtualService{
		Key: svcKey1,
		Config: &types.VirtualService_Config{
			Scheduler: "sh",
			Flags:     []string{"flag-2", "flag-1"},
		},
		Id: "svc1",
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
			Weight:  1.0,
			Forward: "dr",
		},
	}
	server1Updated := &types.RealServer{
		Key: serverKey1,
		Config: &types.RealServer_Config{
			Weight:  0.0,
			Forward: "dr",
		},
	}
	serverKey2 := &types.RealServer_Key{
		Ip:   "172.16.1.2",
		Port: 502,
	}
	server2 := &types.RealServer{
		Key: serverKey2,
		Config: &types.RealServer_Config{
			Weight:  1.0,
			Forward: "dr",
		},
	}

	type servers map[*types.VirtualService][]*types.RealServer

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
	}

	DescribeTable("reconcile", func(c reconcileCase) {
		storeMock := &storeMock{}
		ipvsMock := &ipvsMock{}
		r := New(0, storeMock, ipvsMock).(*reconciler)

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
		for _, svc := range c.desiredServices {
			if _, ok := c.actualServers[svc]; !ok {
				c.actualServers[svc] = []*types.RealServer{}
			}
			if _, ok := c.desiredServers[svc]; !ok {
				c.desiredServers[svc] = []*types.RealServer{}
			}
		}

		// add expectations for store and ipvs
		// services
		ipvsMock.On("ListServices").Return(c.actualServices, nil)
		storeMock.On("ListServices", mock.Anything).Return(c.desiredServices, nil)
		for _, s := range c.createdServices {
			ipvsMock.On("AddService", s).Return(nil)
		}
		for _, s := range c.updatedServices {
			ipvsMock.On("UpdateService", s).Return(nil)
		}
		for _, s := range c.deletedServices {
			ipvsMock.On("DeleteService", s.Key).Return(nil)
		}
		// servers
		for k, v := range c.desiredServers {
			storeMock.On("ListServers", mock.Anything, k.Id).Return(v, nil)
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

		// reconcile
		r.reconcile()

		// ensure expected outcomes
		storeMock.AssertExpectations(GinkgoT())
		ipvsMock.AssertExpectations(GinkgoT())
	},
		cases...)
})

type storeMock struct {
	mock.Mock
}

func (s *storeMock) Close() {
	panic("not implemented")
}
func (s *storeMock) ListServices(ctx context.Context) ([]*types.VirtualService, error) {
	args := s.Mock.Called(ctx)
	return args.Get(0).([]*types.VirtualService), args.Error(1)
}
func (s *storeMock) ListServers(ctx context.Context, serviceID string) ([]*types.RealServer, error) {
	args := s.Mock.Called(ctx, serviceID)
	return args.Get(0).([]*types.RealServer), args.Error(1)
}

type ipvsMock struct {
	mock.Mock
}

func (i *ipvsMock) AddService(svc *types.VirtualService) error {
	args := i.Mock.Called(svc)
	return args.Error(0)
}
func (i *ipvsMock) UpdateService(svc *types.VirtualService) error {
	args := i.Mock.Called(svc)
	return args.Error(0)
}
func (i *ipvsMock) DeleteService(key *types.VirtualService_Key) error {
	args := i.Mock.Called(key)
	return args.Error(0)
}
func (i *ipvsMock) ListServices() ([]*types.VirtualService, error) {
	args := i.Mock.Called()
	return args.Get(0).([]*types.VirtualService), args.Error(1)
}
func (i *ipvsMock) AddServer(key *types.VirtualService_Key, server *types.RealServer) error {
	args := i.Mock.Called(key, server)
	return args.Error(0)
}
func (i *ipvsMock) UpdateServer(key *types.VirtualService_Key, server *types.RealServer) error {
	args := i.Mock.Called(key, server)
	return args.Error(0)
}
func (i *ipvsMock) DeleteServer(key *types.VirtualService_Key, server *types.RealServer) error {
	args := i.Mock.Called(key, server)
	return args.Error(0)
}
func (i *ipvsMock) ListServers(key *types.VirtualService_Key) ([]*types.RealServer, error) {
	args := i.Mock.Called(key)
	return args.Get(0).([]*types.RealServer), args.Error(1)
}
