package ipvs

import (
	"testing"

	"sort"

	"net"
	"syscall"

	"context"

	"github.com/docker/libnetwork/ipvs"
	"github.com/golang/protobuf/ptypes/wrappers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/sky-uk/merlin/types"
	"github.com/stretchr/testify/mock"
)

func TestIPVS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "IPVS Shim Suite")
}

var _ = Describe("IPVS Shim", func() {

	var (
		ipvsShim IPVS
		hMock    *handleMock
		svc      *types.VirtualService
		hSvc     *ipvs.Service
		hSvcKey  *ipvs.Service
		server   *types.RealServer
		hDest    *ipvs.Destination
		hDestKey *ipvs.Destination
		ctx      context.Context
	)

	BeforeEach(func() {
		hMock = &handleMock{}
		ipvsShim = &shim{handle: hMock}

		// virtual service fixtures
		svc = &types.VirtualService{
			Key: &types.VirtualService_Key{
				Ip:       "10.10.10.10",
				Port:     555,
				Protocol: types.Protocol_TCP,
			},
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
				Flags:     []string{"flag-2", "flag-3"},
			},
		}
		hSvc = &ipvs.Service{
			Address:       net.ParseIP("10.10.10.10"),
			Protocol:      syscall.IPPROTO_TCP,
			Port:          555,
			AddressFamily: syscall.AF_INET,
			SchedName:     "sh",
			Flags:         ipVsSvcFSched2 | ipVsSvcFSched3,
		}
		hSvcCopy := *hSvc
		hSvcKey = &hSvcCopy
		hSvcKey.SchedName = ""
		hSvcKey.Flags = 0

		// real server fixtures
		server = &types.RealServer{
			Key: &types.RealServer_Key{
				Ip:   "172.16.10.10",
				Port: 999,
			},
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 2},
				Forward: types.ForwardMethod_MASQ,
			},
		}
		hDest = &ipvs.Destination{
			Address:         net.ParseIP("172.16.10.10"),
			Port:            999,
			AddressFamily:   syscall.AF_INET,
			ConnectionFlags: ipvs.ConnectionFlagMasq,
			Weight:          2,
		}
		hDestCopy := *hDest
		hDestKey = &hDestCopy
		hDestKey.ConnectionFlags = 0
		hDestKey.Weight = 0
		ctx = context.Background()
	})

	Describe("AddService", func() {
		It("should add to libipvs", func() {
			hMock.On("NewService", hSvc).Return(nil)

			err := ipvsShim.AddService(ctx, svc)

			Expect(err).ToNot(HaveOccurred())
			hMock.AssertExpectations(GinkgoT())
		})

		DescribeTable("translate flags", func(flags []string, flagbits uint32) {
			svc.Config.Flags = flags
			hSvc.Flags = flagbits

			hMock.On("NewService", hSvc).Return(nil)

			err := ipvsShim.AddService(ctx, svc)

			Expect(err).ToNot(HaveOccurred())
			hMock.AssertExpectations(GinkgoT())
		},
			Entry("no flags",
				[]string{""},
				uint32(0)),
			Entry("single flag",
				[]string{"flag-1"},
				uint32(ipVsSvcFSched1)),
			Entry("multiple flags",
				[]string{"flag-1", "flag-2"},
				uint32(ipVsSvcFSched1|ipVsSvcFSched2)),
			Entry("invalid flags",
				[]string{"flag-1", "ignored", "flag-2"},
				uint32(ipVsSvcFSched1|ipVsSvcFSched2)),
		)

		It("should support UDP", func() {
			svc.Key.Protocol = types.Protocol_UDP
			hSvc.Protocol = syscall.IPPROTO_UDP
			hMock.On("NewService", hSvc).Return(nil)

			err := ipvsShim.AddService(ctx, svc)

			Expect(err).ToNot(HaveOccurred())
			hMock.AssertExpectations(GinkgoT())
		})
	})

	Describe("UpdateService", func() {
		It("should update in libipvs", func() {
			hMock.On("UpdateService", hSvc).Return(nil)

			err := ipvsShim.UpdateService(ctx, svc)

			Expect(err).ToNot(HaveOccurred())
			hMock.AssertExpectations(GinkgoT())
		})
	})

	Describe("DeleteService", func() {
		It("should delete in libipvs", func() {
			hMock.On("DelService", hSvc).Return(nil)

			hSvc.SchedName = ""
			hSvc.Flags = 0
			err := ipvsShim.DeleteService(ctx, svc.Key)

			Expect(err).ToNot(HaveOccurred())
			hMock.AssertExpectations(GinkgoT())
		})
	})

	Describe("ListServices", func() {
		It("should list all services reported by libipvs", func() {
			hSvcs := []*ipvs.Service{hSvc}
			hMock.On("GetServices").Return(hSvcs, nil)

			svcs, err := ipvsShim.ListServices(ctx)

			Expect(err).ToNot(HaveOccurred())
			Expect(svcs).To(HaveLen(1))
			Expect(svcs).To(ContainElement(svc))
		})
	})

	Describe("AddServer", func() {
		It("should add to libipvs", func() {
			hMock.On("NewDestination", hSvcKey, hDest).Return(nil)

			err := ipvsShim.AddServer(ctx, svc.Key, server)

			Expect(err).ToNot(HaveOccurred())
			hMock.AssertExpectations(GinkgoT())
		})
	})

	Describe("UpdateServer", func() {
		It("should update in libipvs", func() {
			hMock.On("UpdateDestination", hSvcKey, hDest).Return(nil)

			err := ipvsShim.UpdateServer(ctx, svc.Key, server)

			Expect(err).ToNot(HaveOccurred())
			hMock.AssertExpectations(GinkgoT())
		})
	})

	Describe("DeleteServer", func() {
		It("should delete in libipvs", func() {
			hMock.On("DelDestination", hSvcKey, hDestKey).Return(nil)

			err := ipvsShim.DeleteServer(ctx, svc.Key, server)

			Expect(err).ToNot(HaveOccurred())
			hMock.AssertExpectations(GinkgoT())
		})
	})

	Describe("ListServers", func() {
		It("should list all the destinations in libipvs", func() {
			hMock.On("GetDestinations", hSvcKey).Return([]*ipvs.Destination{hDest}, nil)

			servers, err := ipvsShim.ListServers(ctx, svc.Key)

			Expect(err).ToNot(HaveOccurred())
			Expect(servers).To(HaveLen(1))
			Expect(servers).To(ContainElement(server))
		})
	})

	DescribeTable("Flagbits Conversion", func(flagbits int, flags []string) {
		sort.Strings(flags)
		actualFlags := fromFlagBits(uint32(flagbits))
		Expect(actualFlags).To(Equal(flags))

		actualFlagbits := toFlagBits(flags)
		Expect(actualFlagbits).To(Equal(uint32(flagbits)))
	},
		Entry("flag-1", ipVsSvcFSched1, []string{"flag-1"}),
		Entry("flag-2", ipVsSvcFSched2, []string{"flag-2"}),
		Entry("flag-3", ipVsSvcFSched3, []string{"flag-3"}),
		Entry("multiple flags", ipVsSvcFSched1|ipVsSvcFSched2|ipVsSvcFSched3,
			[]string{"flag-1", "flag-2", "flag-3"}))
})

type handleMock struct {
	mock.Mock
}

func (m *handleMock) Close() {
	m.Called()
}

func (m *handleMock) GetServices() (services []*ipvs.Service, err error) {
	args := m.Called()
	return args.Get(0).([]*ipvs.Service), args.Error(1)
}

func (m *handleMock) NewService(s *ipvs.Service) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *handleMock) UpdateService(s *ipvs.Service) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *handleMock) DelService(s *ipvs.Service) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *handleMock) GetDestinations(s *ipvs.Service) ([]*ipvs.Destination, error) {
	args := m.Called(s)
	return args.Get(0).([]*ipvs.Destination), args.Error(1)
}

func (m *handleMock) NewDestination(s *ipvs.Service, d *ipvs.Destination) error {
	args := m.Called(s, d)
	return args.Error(0)
}

func (m *handleMock) UpdateDestination(s *ipvs.Service, d *ipvs.Destination) error {
	args := m.Called(s, d)
	return args.Error(0)
}

func (m *handleMock) DelDestination(s *ipvs.Service, d *ipvs.Destination) error {
	args := m.Called(s, d)
	return args.Error(0)
}
