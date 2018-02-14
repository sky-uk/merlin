package ipvs

import (
	"reflect"
	"testing"

	"sort"

	"github.com/mqliang/libipvs"

	"net"
	"syscall"

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

var (
	ipvs     IPVS
	hMock    *handlerMock
	svc      *types.VirtualService
	hSvc     *libipvs.Service
	hSvcKey  *libipvs.Service
	server   *types.RealServer
	hDest    *libipvs.Destination
	hDestKey *libipvs.Destination
)

var _ = BeforeEach(func() {
	hMock = &handlerMock{}
	ipvs = New(hMock)

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
	hSvc = &libipvs.Service{
		Address:       net.ParseIP("10.10.10.10"),
		Protocol:      libipvs.Protocol(syscall.IPPROTO_TCP),
		Port:          uint16(555),
		AddressFamily: syscall.AF_INET,
		SchedName:     "sh",
		Flags: libipvs.Flags{
			Flags: libipvs.IP_VS_SVC_F_SCHED2 | libipvs.IP_VS_SVC_F_SCHED3,
			Mask:  ^uint32(0),
		},
	}
	hSvcCopy := *hSvc
	hSvcKey = &hSvcCopy
	hSvcKey.SchedName = ""
	hSvcKey.Flags = libipvs.Flags{}

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
	hDest = &libipvs.Destination{
		Address:       net.ParseIP("172.16.10.10"),
		Port:          999,
		AddressFamily: syscall.AF_INET,
		FwdMethod:     libipvs.FwdMethod(libipvs.IP_VS_CONN_F_MASQ),
		Weight:        2,
	}
	hDestCopy := *hDest
	hDestKey = &hDestCopy
	hDestKey.FwdMethod = 0
	hDestKey.Weight = 0
})

var _ = Describe("AddService", func() {

	It("should add to libipvs", func() {
		hMock.On("NewService", hSvc).Return(nil)

		err := ipvs.AddService(svc)

		Expect(err).ToNot(HaveOccurred())
		hMock.AssertExpectations(GinkgoT())
	})

	DescribeTable("translate flags", func(flags []string, flagbits uint32) {
		svc.Config.Flags = flags
		hSvc.Flags.Flags = flagbits

		hMock.On("NewService", hSvc).Return(nil)

		err := ipvs.AddService(svc)

		Expect(err).ToNot(HaveOccurred())
		hMock.AssertExpectations(GinkgoT())
	},
		Entry("no flags",
			[]string{""},
			uint32(0)),
		Entry("single flag",
			[]string{"flag-1"},
			uint32(libipvs.IP_VS_SVC_F_SCHED1)),
		Entry("multiple flags",
			[]string{"flag-1", "flag-2"},
			uint32(libipvs.IP_VS_SVC_F_SCHED1|libipvs.IP_VS_SVC_F_SCHED2)),
		Entry("invalid flags",
			[]string{"flag-1", "ignored", "flag-2"},
			uint32(libipvs.IP_VS_SVC_F_SCHED1|libipvs.IP_VS_SVC_F_SCHED2)),
	)

	It("should support UDP", func() {
		svc.Key.Protocol = types.Protocol_UDP
		hSvc.Protocol = libipvs.Protocol(syscall.IPPROTO_UDP)
		hMock.On("NewService", hSvc).Return(nil)

		err := ipvs.AddService(svc)

		Expect(err).ToNot(HaveOccurred())
		hMock.AssertExpectations(GinkgoT())
	})
})

var _ = Describe("UpdateService", func() {
	It("should update in libipvs", func() {
		hMock.On("UpdateService", hSvc).Return(nil)

		err := ipvs.UpdateService(svc)

		Expect(err).ToNot(HaveOccurred())
		hMock.AssertExpectations(GinkgoT())
	})
})

var _ = Describe("DeleteService", func() {
	It("should delete in libipvs", func() {
		hMock.On("DelService", hSvc).Return(nil)

		hSvc.SchedName = ""
		hSvc.Flags = libipvs.Flags{}
		err := ipvs.DeleteService(svc.Key)

		Expect(err).ToNot(HaveOccurred())
		hMock.AssertExpectations(GinkgoT())
	})
})

var _ = Describe("ListServices", func() {
	It("should list all services reported by libipvs", func() {
		hSvcs := []*libipvs.Service{hSvc}
		hMock.On("ListServices").Return(hSvcs, nil)

		svcs, err := ipvs.ListServices()

		Expect(err).ToNot(HaveOccurred())
		Expect(svcs).To(HaveLen(1))
		Expect(svcs).To(ContainElement(svc))
	})
})

var _ = Describe("AddServer", func() {
	It("should add to libipvs", func() {
		hMock.On("NewDestination", hSvcKey, hDest).Return(nil)

		err := ipvs.AddServer(svc.Key, server)

		Expect(err).ToNot(HaveOccurred())
		hMock.AssertExpectations(GinkgoT())
	})
})

var _ = Describe("UpdateServer", func() {
	It("should update in libipvs", func() {
		hMock.On("UpdateDestination", hSvcKey, hDest).Return(nil)

		err := ipvs.UpdateServer(svc.Key, server)

		Expect(err).ToNot(HaveOccurred())
		hMock.AssertExpectations(GinkgoT())
	})
})

var _ = Describe("DeleteServer", func() {
	It("should delete in libipvs", func() {
		hMock.On("DelDestination", hSvcKey, hDestKey).Return(nil)

		err := ipvs.DeleteServer(svc.Key, server)

		Expect(err).ToNot(HaveOccurred())
		hMock.AssertExpectations(GinkgoT())
	})
})

var _ = Describe("ListServers", func() {
	It("should list all the destinations in libipvs", func() {
		hMock.On("ListDestinations", hSvcKey).Return([]*libipvs.Destination{hDest}, nil)

		servers, err := ipvs.ListServers(svc.Key)

		Expect(err).ToNot(HaveOccurred())
		Expect(servers).To(HaveLen(1))
		Expect(servers).To(ContainElement(server))
	})
})

type handlerMock struct {
	mock.Mock
}

func (m *handlerMock) Flush() error {
	panic("implement me")
}

func (m *handlerMock) GetInfo() (info libipvs.Info, err error) {
	panic("implement me")
}

func (m *handlerMock) ListServices() (services []*libipvs.Service, err error) {
	args := m.Called()
	return args.Get(0).([]*libipvs.Service), args.Error(1)
}

func (m *handlerMock) NewService(s *libipvs.Service) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *handlerMock) UpdateService(s *libipvs.Service) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *handlerMock) DelService(s *libipvs.Service) error {
	args := m.Called(s)
	return args.Error(0)
}

func (m *handlerMock) ListDestinations(s *libipvs.Service) (dsts []*libipvs.Destination, err error) {
	args := m.Called(s)
	return args.Get(0).([]*libipvs.Destination), args.Error(1)
}

func (m *handlerMock) NewDestination(s *libipvs.Service, d *libipvs.Destination) error {
	args := m.Called(s, d)
	return args.Error(0)
}

func (m *handlerMock) UpdateDestination(s *libipvs.Service, d *libipvs.Destination) error {
	args := m.Called(s, d)
	return args.Error(0)
}

func (m *handlerMock) DelDestination(s *libipvs.Service, d *libipvs.Destination) error {
	args := m.Called(s, d)
	return args.Error(0)
}

func TestConvertFlagbits(t *testing.T) {
	tests := []struct {
		name     string
		flagbits uint32
		want     []string
	}{
		{
			"flag-1",
			libipvs.IP_VS_SVC_F_SCHED1,
			[]string{"flag-1"},
		},
		{
			"flag-2",
			libipvs.IP_VS_SVC_F_SCHED2,
			[]string{"flag-2"},
		},
		{
			"flag-3",
			libipvs.IP_VS_SVC_F_SCHED3,
			[]string{"flag-3"},
		},
		{
			"multiple flags",
			libipvs.IP_VS_SVC_F_SCHED1 | libipvs.IP_VS_SVC_F_SCHED2 | libipvs.IP_VS_SVC_F_SCHED3,
			[]string{"flag-1", "flag-2", "flag-3"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertFlagbits(tt.flagbits)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("convertFlagbits() = %v, want %v", got, tt.want)
			}
		})
	}
}
