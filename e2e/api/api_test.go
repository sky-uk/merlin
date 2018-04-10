package api

import (
	"context"
	"fmt"
	"time"

	"testing"

	"os"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
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
			Expect(status.Code()).To(Equal(codes.InvalidArgument),
				"expected InvalidArgument, but got %v", err)
		},
			Entry("empty service", &types.VirtualService{}),
			Entry("missing id", &types.VirtualService{
				Key:    validKey,
				Config: validConfig,
			}),
			Entry("missing IP", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Port:     8080,
					Protocol: types.Protocol_TCP,
				},
				Config: validConfig,
			}),
			Entry("invalid IP", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:       "999.999.999.999",
					Port:     8080,
					Protocol: types.Protocol_TCP,
				},
				Config: validConfig,
			}),
			Entry("invalid port", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:       "127.0.0.1",
					Port:     99999,
					Protocol: types.Protocol_TCP,
				},
				Config: validConfig,
			}),
			Entry("zero port", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:       "127.0.0.1",
					Port:     0,
					Protocol: types.Protocol_TCP,
				},
				Config: validConfig,
			}),
			Entry("missing protocol", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:   "127.0.0.1",
					Port: 8080,
				},
				Config: validConfig,
			}),
			Entry("invalid protocol", &types.VirtualService{
				Id: "service1",
				Key: &types.VirtualService_Key{
					Ip:       "127.0.0.1",
					Port:     8080,
					Protocol: 999,
				},
				Config: validConfig,
			}),
			Entry("config required", &types.VirtualService{
				Id:  "service1",
				Key: validKey,
			}),
			Entry("scheduler required", &types.VirtualService{
				Id:     "service1",
				Key:    validKey,
				Config: &types.VirtualService_Config{},
			}),
		)

		It("should return codes.AlreadyExists if already exists", func() {
			svc1 := &types.VirtualService{
				Id:     "service1",
				Key:    validKey,
				Config: validConfig,
			}
			svc2 := &types.VirtualService{
				Id:  svc1.Id,
				Key: validKey,
				Config: &types.VirtualService_Config{
					Scheduler: "wrr",
				},
			}

			_, err := client.CreateService(ctx, svc1)
			Expect(err).ToNot(HaveOccurred())
			_, err = client.CreateService(ctx, svc2)
			status, ok := status.FromError(err)

			Expect(ok).To(BeTrue(), "got grpc status error")
			Expect(status.Code()).To(Equal(codes.AlreadyExists),
				"expected AlreadyExists, but got %v", err)
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
			Expect(status.Code()).To(Equal(codes.NotFound),
				"expected NotFound, but got %v", err)
		})
	})

	Context("Real Servers", func() {

		var (
			service = &types.VirtualService{
				Id:     "service1",
				Key:    validKey,
				Config: validConfig,
			}
			validServerKey = &types.RealServer_Key{
				Ip:   "172.16.1.1",
				Port: 9090,
			}
			validServerConfig = &types.RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 2},
				Forward: types.ForwardMethod_ROUTE,
			}
		)

		BeforeEach(func() {
			_, err := client.CreateService(ctx, service)
			Expect(err).ToNot(HaveOccurred())
		})

		Describe("CreateServer", func() {

			DescribeTable("field validation", func(server *types.RealServer) {
				_, err := client.CreateServer(ctx, server)
				status, ok := status.FromError(err)

				Expect(ok).To(BeTrue(), "got grpc status error")
				Expect(status.Code()).To(Equal(codes.InvalidArgument),
					"expected InvalidArgument, but got %v", err)
			},
				Entry("empty server", &types.RealServer{}),
				Entry("missing serviceID", &types.RealServer{
					Key:    validServerKey,
					Config: validServerConfig,
				}),
				Entry("missing key", &types.RealServer{
					ServiceID: service.Id,
					Config:    validServerConfig,
				}),
				Entry("missing ip", &types.RealServer{
					ServiceID: service.Id,
					Key: &types.RealServer_Key{
						Port: 9090,
					},
					Config: validServerConfig,
				}),
				Entry("invalid ip", &types.RealServer{
					ServiceID: service.Id,
					Key: &types.RealServer_Key{
						Ip:   "999.999.999.999",
						Port: 9090,
					},
					Config: validServerConfig,
				}),
				Entry("invalid port", &types.RealServer{
					ServiceID: service.Id,
					Key: &types.RealServer_Key{
						Ip:   "127.0.0.1",
						Port: 99999,
					},
					Config: validServerConfig,
				}),
				Entry("missing port", &types.RealServer{
					ServiceID: service.Id,
					Key: &types.RealServer_Key{
						Ip: "172.16.1.1",
					},
					Config: validServerConfig,
				}),
				Entry("missing config", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
				}),
				Entry("missing forward method", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config: &types.RealServer_Config{
						Weight: &wrappers.UInt32Value{Value: 2},
					},
				}),
				Entry("missing weight", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config: &types.RealServer_Config{
						Forward: types.ForwardMethod_ROUTE,
					},
				}),
				Entry("invalid health check scheme", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config:    validServerConfig,
					HealthCheck: &types.RealServer_HealthCheck{
						Endpoint:      &wrappers.StringValue{Value: "bogus://:556/health"},
						Period:        ptypes.DurationProto(time.Second),
						Timeout:       ptypes.DurationProto(time.Second),
						DownThreshold: 1,
						UpThreshold:   1,
					},
				}),
				Entry("missing health check period", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config:    validServerConfig,
					HealthCheck: &types.RealServer_HealthCheck{
						Endpoint:      &wrappers.StringValue{Value: "http://:556/health"},
						Timeout:       ptypes.DurationProto(time.Second),
						DownThreshold: 1,
						UpThreshold:   1,
					},
				}),
				Entry("missing health check timeout", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config:    validServerConfig,
					HealthCheck: &types.RealServer_HealthCheck{
						Endpoint:      &wrappers.StringValue{Value: "http://:556/health"},
						Period:        ptypes.DurationProto(time.Second),
						DownThreshold: 1,
						UpThreshold:   1,
					},
				}),
				Entry("missing health check down threshold", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config:    validServerConfig,
					HealthCheck: &types.RealServer_HealthCheck{
						Endpoint:    &wrappers.StringValue{Value: "http://:556/health"},
						Period:      ptypes.DurationProto(time.Second),
						Timeout:     ptypes.DurationProto(time.Second),
						UpThreshold: 1,
					},
				}),
				Entry("missing health check up threshold", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config:    validServerConfig,
					HealthCheck: &types.RealServer_HealthCheck{
						Endpoint:      &wrappers.StringValue{Value: "http://:556/health"},
						Period:        ptypes.DurationProto(time.Second),
						Timeout:       ptypes.DurationProto(time.Second),
						DownThreshold: 1,
					},
				}),
			)

			It("should return codes.AlreadyExists if already exists", func() {
				server1 := &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config:    validServerConfig,
				}
				server2 := proto.Clone(server1).(*types.RealServer)
				server2.Config.Forward = types.ForwardMethod_TUNNEL

				_, err := client.CreateServer(ctx, server1)
				Expect(err).ToNot(HaveOccurred())
				_, err = client.CreateServer(ctx, server2)
				status, ok := status.FromError(err)

				Expect(ok).To(BeTrue(), "got grpc status error")
				Expect(status.Code()).To(Equal(codes.AlreadyExists),
					"expected AlreadyExists, but got %v", err)
			})

			It("should return codes.NotFound if service doesn't exist", func() {
				server := &types.RealServer{
					ServiceID: "service-does-not-exist",
					Key:       validServerKey,
					Config:    validServerConfig,
				}
				_, err := client.CreateServer(ctx, server)
				fmt.Println(err)
				status, ok := status.FromError(err)

				Expect(ok).To(BeTrue(), "got grpc status error")
				Expect(status.Code()).To(Equal(codes.NotFound),
					"expected NotFound, but got %v", err)
			})
		})

		Describe("UpdateServer", func() {

			// test all invalid / missing fields (missing key, missing service, missing server)
			DescribeTable("should return codes.NotFound if doesn't exist", func(update *types.RealServer) {
				server := &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config:    validServerConfig,
				}
				_, err := client.CreateServer(ctx, server)
				Expect(err).ToNot(HaveOccurred())

				_, err = client.UpdateServer(ctx, update)
				status, ok := status.FromError(err)

				Expect(ok).To(BeTrue(), "got grpc status error")
				Expect(status.Code()).To(Equal(codes.NotFound),
					"expected NotFound, but got %v", err)
			},
				Entry("no existing server", &types.RealServer{
					ServiceID: service.Id,
					Key: &types.RealServer_Key{
						Ip:   "172.16.20.5",
						Port: 7474,
					},
					Config: &types.RealServer_Config{Forward: types.ForwardMethod_MASQ},
				}),
				Entry("no existing service", &types.RealServer{
					ServiceID: "nonexistent_service",
					Key:       validServerKey,
					Config:    &types.RealServer_Config{Forward: types.ForwardMethod_MASQ},
				}),
				Entry("missing key", &types.RealServer{
					ServiceID: service.Id,
					Config:    &types.RealServer_Config{Forward: types.ForwardMethod_MASQ},
				}),
			)

			DescribeTable("should be able to update individual fields", func(update *types.RealServer) {
				server := &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config:    validServerConfig,
					HealthCheck: &types.RealServer_HealthCheck{
						Endpoint:      &wrappers.StringValue{Value: "http://:556/health"},
						Period:        ptypes.DurationProto(time.Second),
						Timeout:       ptypes.DurationProto(time.Second),
						DownThreshold: 1,
						UpThreshold:   1,
					},
				}
				_, err := client.CreateServer(ctx, server)
				Expect(err).ToNot(HaveOccurred())

				_, err = client.UpdateServer(ctx, update)
				Expect(err).ToNot(HaveOccurred())
			},
				Entry("change weight", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config: &types.RealServer_Config{
						Weight: &wrappers.UInt32Value{4},
					},
				}),
				Entry("change forward", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					Config: &types.RealServer_Config{
						Forward: types.ForwardMethod_MASQ,
					},
				}),
				Entry("change health check endpoint", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					HealthCheck: &types.RealServer_HealthCheck{
						Endpoint: &wrappers.StringValue{Value: "http://:999/health2"},
					},
				}),
				Entry("change health check period", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					HealthCheck: &types.RealServer_HealthCheck{
						Period: ptypes.DurationProto(time.Minute),
					},
				}),
				Entry("change health check timeout", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					HealthCheck: &types.RealServer_HealthCheck{
						Timeout: ptypes.DurationProto(time.Minute),
					},
				}),
				Entry("change health check up threshold", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					HealthCheck: &types.RealServer_HealthCheck{
						UpThreshold: 5,
					},
				}),
				Entry("change health check down threshold", &types.RealServer{
					ServiceID: service.Id,
					Key:       validServerKey,
					HealthCheck: &types.RealServer_HealthCheck{
						DownThreshold: 4,
					},
				}),
			)
		})
	})
})
