package api

import (
	"context"
	"fmt"
	"time"

	"testing"

	"os"

	"github.com/golang/protobuf/ptypes/wrappers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	. "github.com/sky-uk/merlin/e2e"
	"github.com/sky-uk/merlin/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestE2EAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E API Suite")
}

var _ = Describe("server API validation", func() {
	BeforeSuite(func() {
		SetupE2E()
		StartEtcd()
		StartMerlin()
	})

	AfterSuite(func() {
		StopEtcd()
		StopMerlin()
	})

	var (
		conn       *grpc.ClientConn
		client     types.MerlinClient
		ctx        context.Context
		cancelCall context.CancelFunc
	)

	BeforeEach(func() {
		dest := fmt.Sprintf("localhost:%s", MerlinPort())
		fmt.Fprintf(os.Stderr, "dialing %s\n", dest)
		c, err := grpc.Dial(dest, grpc.WithInsecure())
		if err != nil {
			panic(err)
		}
		conn = c
		client = types.NewMerlinClient(conn)

		ctx, cancelCall = context.WithTimeout(context.Background(), time.Second*10)
	})

	AfterEach(func() {
		cancelCall()
		conn.Close()
	})

	DescribeTable("CreateService", func(service *types.VirtualService) {
		_, err := client.CreateService(ctx, service)
		status, ok := status.FromError(err)

		Expect(ok).To(BeTrue(), "got grpc status error")
		if ok {
			Expect(status.Code()).To(Equal(codes.InvalidArgument),
				"expected InvalidArgument, but got %v", err)
		}
	},
		Entry("empty service", &types.VirtualService{}),
		Entry("missing id", &types.VirtualService{
			Key: &types.VirtualService_Key{
				Ip:       "127.0.0.1",
				Port:     8080,
				Protocol: types.Protocol_TCP,
			},
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
			},
		}),
		Entry("missing IP", &types.VirtualService{
			Id: "service1",
			Key: &types.VirtualService_Key{
				Port:     8080,
				Protocol: types.Protocol_TCP,
			},
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
			},
		}),
		Entry("invalid IP", &types.VirtualService{
			Id: "service1",
			Key: &types.VirtualService_Key{
				Ip:       "999.999.999.999",
				Port:     8080,
				Protocol: types.Protocol_TCP,
			},
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
			},
		}),
		Entry("invalid port", &types.VirtualService{
			Id: "service1",
			Key: &types.VirtualService_Key{
				Ip:       "127.0.0.1",
				Port:     99999,
				Protocol: types.Protocol_TCP,
			},
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
			},
		}),
		Entry("zero port", &types.VirtualService{
			Id: "service1",
			Key: &types.VirtualService_Key{
				Ip:       "127.0.0.1",
				Port:     0,
				Protocol: types.Protocol_TCP,
			},
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
			},
		}),
		Entry("missing protocol", &types.VirtualService{
			Id: "service1",
			Key: &types.VirtualService_Key{
				Ip:   "127.0.0.1",
				Port: 8080,
			},
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
			},
		}),
		Entry("invalid protocol", &types.VirtualService{
			Id: "service1",
			Key: &types.VirtualService_Key{
				Ip:       "127.0.0.1",
				Port:     8080,
				Protocol: 999,
			},
			Config: &types.VirtualService_Config{
				Scheduler: "sh",
			},
		}),
		Entry("config required", &types.VirtualService{
			Id: "service1",
			Key: &types.VirtualService_Key{
				Ip:       "127.0.0.1",
				Port:     8080,
				Protocol: types.Protocol_TCP,
			},
		}),
		Entry("scheduler required", &types.VirtualService{
			Id: "service1",
			Key: &types.VirtualService_Key{
				Ip:       "127.0.0.1",
				Port:     8080,
				Protocol: types.Protocol_TCP,
			},
			Config: &types.VirtualService_Config{},
		}),
	)

	DescribeTable("CreateServer", func(server *types.RealServer) {
		_, err := client.CreateServer(ctx, server)
		status, ok := status.FromError(err)

		Expect(ok).To(BeTrue(), "got grpc status error")
		if ok {
			Expect(status.Code()).To(Equal(codes.InvalidArgument),
				"expected InvalidArgument, but got %v", err)
		}
	},
		Entry("empty server", &types.RealServer{}),
		Entry("missing serviceID", &types.RealServer{
			Key: &types.RealServer_Key{
				Ip:   "172.16.1.1",
				Port: 9090,
			},
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 2},
				Forward: types.ForwardMethod_ROUTE,
			},
		}),
		Entry("missing key", &types.RealServer{
			ServiceID: "service1",
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 2},
				Forward: types.ForwardMethod_ROUTE,
			},
		}),
		Entry("missing ip", &types.RealServer{
			ServiceID: "service1",
			Key: &types.RealServer_Key{
				Port: 9090,
			},
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 2},
				Forward: types.ForwardMethod_ROUTE,
			},
		}),
		Entry("invalid ip", &types.RealServer{
			ServiceID: "service1",
			Key: &types.RealServer_Key{
				Ip:   "999.999.999.999",
				Port: 9090,
			},
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 2},
				Forward: types.ForwardMethod_ROUTE,
			},
		}),
		Entry("invalid port", &types.RealServer{
			ServiceID: "service1",
			Key: &types.RealServer_Key{
				Ip:   "127.0.0.1",
				Port: 99999,
			},
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 2},
				Forward: types.ForwardMethod_ROUTE,
			},
		}),
		Entry("missing port", &types.RealServer{
			ServiceID: "service1",
			Key: &types.RealServer_Key{
				Ip: "172.16.1.1",
			},
			Config: &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 2},
				Forward: types.ForwardMethod_ROUTE,
			},
		}),
		Entry("missing config", &types.RealServer{
			ServiceID: "service1",
			Key: &types.RealServer_Key{
				Ip:   "172.16.1.1",
				Port: 9090,
			},
		}),
		Entry("missing forward method", &types.RealServer{
			ServiceID: "service1",
			Key: &types.RealServer_Key{
				Ip:   "172.16.1.1",
				Port: 9090,
			},
			Config: &types.RealServer_Config{
				Weight: &wrappers.UInt32Value{Value: 2},
			},
		}),
		Entry("missing weight", &types.RealServer{
			ServiceID: "service1",
			Key: &types.RealServer_Key{
				Ip:   "172.16.1.1",
				Port: 9090,
			},
			Config: &types.RealServer_Config{
				Forward: types.ForwardMethod_ROUTE,
			},
		}),
	)
})
