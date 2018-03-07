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

var _ = Describe("API", func() {
	BeforeSuite(func() {
		SetupE2E()
	})

	var (
		conn       *grpc.ClientConn
		client     types.MerlinClient
		ctx        context.Context
		cancelCall context.CancelFunc

		validKey = &types.VirtualService_Key{
			Ip:       "127.0.0.1",
			Port:     8080,
			Protocol: types.Protocol_TCP,
		}
		validConfig = &types.VirtualService_Config{
			Scheduler: "sh",
		}
		validRealServerConfig = &types.VirtualService_RealServerConfig{
			ForwardPort:   900,
			ForwardMethod: types.ForwardMethod_MASQ,
		}
	)

	BeforeEach(func() {
		StartEtcd()
		StartMerlin()

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
		StopEtcd()
		StopMerlin()
	})

	Describe("CreateService", func() {

		DescribeTable("field validation", func(service *types.VirtualService) {
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
				Key:              validKey,
				Config:           validConfig,
				RealServerConfig: validRealServerConfig,
			}),
			Entry("missing IP", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Port:     8080,
					Protocol: types.Protocol_TCP,
				},
				Config:           validConfig,
				RealServerConfig: validRealServerConfig,
			}),
			Entry("invalid IP", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:       "999.999.999.999",
					Port:     8080,
					Protocol: types.Protocol_TCP,
				},
				Config:           validConfig,
				RealServerConfig: validRealServerConfig,
			}),
			Entry("invalid port", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:       "127.0.0.1",
					Port:     99999,
					Protocol: types.Protocol_TCP,
				},
				Config:           validConfig,
				RealServerConfig: validRealServerConfig,
			}),
			Entry("zero port", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:       "127.0.0.1",
					Port:     0,
					Protocol: types.Protocol_TCP,
				},
				Config:           validConfig,
				RealServerConfig: validRealServerConfig,
			}),
			Entry("missing protocol", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:   "127.0.0.1",
					Port: 8080,
				},
				Config:           validConfig,
				RealServerConfig: validRealServerConfig,
			}),
			Entry("invalid protocol", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:       "127.0.0.1",
					Port:     8080,
					Protocol: 999,
				},
				Config:           validConfig,
				RealServerConfig: validRealServerConfig,
			}),
			Entry("config required", &types.VirtualService{
				Id:               "service1",
				Key:              validKey,
				RealServerConfig: validRealServerConfig,
			}),
			Entry("scheduler required", &types.VirtualService{
				Id:               "service1",
				Key:              validKey,
				Config:           &types.VirtualService_Config{},
				RealServerConfig: validRealServerConfig,
			}),
			Entry("real server config required", &types.VirtualService{
				Id:     "service1",
				Key:    validKey,
				Config: validConfig,
			}),
			Entry("forward port required", &types.VirtualService{
				Id:     "service1",
				Key:    validKey,
				Config: validConfig,
				RealServerConfig: &types.VirtualService_RealServerConfig{
					ForwardMethod: types.ForwardMethod_MASQ,
				},
			}),
			Entry("forward method required", &types.VirtualService{
				Id:     "service1",
				Key:    validKey,
				Config: validConfig,
				RealServerConfig: &types.VirtualService_RealServerConfig{
					ForwardPort: 900,
				},
			}),
		)

		It("should return codes.AlreadyExists if already exists", func() {
			svc1 := &types.VirtualService{
				Id:               "service1",
				Key:              validKey,
				Config:           validConfig,
				RealServerConfig: validRealServerConfig,
			}
			svc2 := &types.VirtualService{
				Id:  svc1.Id,
				Key: validKey,
				Config: &types.VirtualService_Config{
					Scheduler: "wrr",
				},
				RealServerConfig: validRealServerConfig,
			}

			_, err := client.CreateService(ctx, svc1)
			Expect(err).ToNot(HaveOccurred())
			_, err = client.CreateService(ctx, svc2)
			status, ok := status.FromError(err)

			Expect(ok).To(BeTrue(), "got grpc status error")
			if ok {
				Expect(status.Code()).To(Equal(codes.AlreadyExists),
					"expected AlreadyExists, but got %v", err)
			}
		})
	})

	Describe("UpdateService", func() {

		It("should return codes.NotFound if no existing service", func() {
			svc := &types.VirtualService{
				Id:     "service1",
				Key:    validKey,
				Config: validConfig,
			}
			_, err := client.UpdateService(ctx, svc)
			status, ok := status.FromError(err)

			Expect(ok).To(BeTrue(), "got grpc status error")
			if ok {
				Expect(status.Code()).To(Equal(codes.NotFound),
					"expected NotFound, but got %v", err)
			}
		})
	})

	Describe("CreateServer", func() {

		var (
			service = &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:       "127.0.0.1",
					Port:     8080,
					Protocol: types.Protocol_TCP,
				},
				Config: &types.VirtualService_Config{
					Scheduler: "sh",
				},
				RealServerConfig: &types.VirtualService_RealServerConfig{
					ForwardPort:   900,
					ForwardMethod: types.ForwardMethod_MASQ,
				},
			}
		)

		BeforeEach(func() {
			_, err := client.CreateService(ctx, service)
			Expect(err).ToNot(HaveOccurred())
		})

		DescribeTable("field validation", func(server *types.RealServer) {
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
					Ip: "172.16.1.1",
				},
				Config: &types.RealServer_Config{
					Weight: &wrappers.UInt32Value{Value: 2},
				},
			}),
			Entry("missing key", &types.RealServer{
				ServiceID: service.Id,
				Config: &types.RealServer_Config{
					Weight: &wrappers.UInt32Value{Value: 2},
				},
			}),
			Entry("missing ip", &types.RealServer{
				ServiceID: service.Id,
				Key:       &types.RealServer_Key{},
				Config: &types.RealServer_Config{
					Weight: &wrappers.UInt32Value{Value: 2},
				},
			}),
			Entry("invalid ip", &types.RealServer{
				ServiceID: service.Id,
				Key: &types.RealServer_Key{
					Ip: "999.999.999.999",
				},
				Config: &types.RealServer_Config{
					Weight: &wrappers.UInt32Value{Value: 2},
				},
			}),
			Entry("missing config", &types.RealServer{
				ServiceID: service.Id,
				Key: &types.RealServer_Key{
					Ip:   "172.16.1.1",
					Port: 9090,
				},
			}),
			Entry("missing weight", &types.RealServer{
				ServiceID: service.Id,
				Key: &types.RealServer_Key{
					Ip:   "172.16.1.1",
					Port: 9090,
				},
				Config: &types.RealServer_Config{},
			}),
		)

		It("should return codes.AlreadyExists if already exists", func() {
			server1 := &types.RealServer{
				ServiceID: service.Id,
				Key: &types.RealServer_Key{
					Ip: "172.16.1.1",
				},
				Config: &types.RealServer_Config{
					Weight: &wrappers.UInt32Value{Value: 2},
				},
			}
			server2 := &types.RealServer{
				ServiceID: service.Id,
				Key:       server1.Key,
				Config: &types.RealServer_Config{
					Weight: &wrappers.UInt32Value{Value: 1},
				},
			}
			_, err := client.CreateServer(ctx, server1)
			Expect(err).ToNot(HaveOccurred())
			_, err = client.CreateServer(ctx, server2)
			status, ok := status.FromError(err)

			Expect(ok).To(BeTrue(), "got grpc status error")
			if ok {
				Expect(status.Code()).To(Equal(codes.AlreadyExists),
					"expected AlreadyExists, but got %v", err)
			}
		})

		It("should return codes.NotFound if service doesn't exist", func() {
			server := &types.RealServer{
				ServiceID: "service-does-not-exist",
				Key: &types.RealServer_Key{
					Ip: "172.16.1.1",
				},
				Config: &types.RealServer_Config{
					Weight: &wrappers.UInt32Value{Value: 2},
				},
			}
			_, err := client.CreateServer(ctx, server)
			fmt.Println(err)
			status, ok := status.FromError(err)

			Expect(ok).To(BeTrue(), "got grpc status error")
			if ok {
				Expect(status.Code()).To(Equal(codes.NotFound),
					"expected NotFound, but got %v", err)
			}

		})
	})

	Describe("UpdateServer", func() {

		It("should return codes.NotFound if doesn't exist", func() {
			server := &types.RealServer{
				ServiceID: "service1",
				Key: &types.RealServer_Key{
					Ip: "172.16.1.1",
				},
				Config: &types.RealServer_Config{
					Weight: &wrappers.UInt32Value{Value: 2},
				},
			}
			_, err := client.UpdateServer(ctx, server)
			status, ok := status.FromError(err)

			Expect(ok).To(BeTrue(), "got grpc status error")
			if ok {
				Expect(status.Code()).To(Equal(codes.NotFound),
					"expected NotFound, but got %v", err)
			}
		})
	})
})
