package types

import (
	"fmt"
	"testing"

	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestUtil(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Util Suite")
}

var _ = Describe("Util", func() {
	It("pretty prints VirtualService", func() {
		svc := &VirtualService{
			Id: "http-proxy",
			Key: &VirtualService_Key{
				Ip:       "10.10.10.1",
				Port:     101,
				Protocol: Protocol_TCP,
			},
			Config: &VirtualService_Config{
				Scheduler: "sh",
				Flags:     []string{"flag-1", "flag-2"},
			},
		}

		str := svc.PrettyString()
		Expect(str).ToNot(BeEmpty())
		fmt.Println(str)
	})

	It("pretty prints RealServer", func() {
		server := &RealServer{
			ServiceID: "http-proxy",
			Key: &RealServer_Key{
				Ip:   "172.16.1.1",
				Port: 501,
			},
			Config: &RealServer_Config{
				Weight:  &wrappers.UInt32Value{Value: 1},
				Forward: ForwardMethod_ROUTE,
			},
			HealthCheck: &RealServer_HealthCheck{
				Endpoint:      &wrappers.StringValue{Value: "http://:102/health"},
				Period:        ptypes.DurationProto(10 * time.Second),
				Timeout:       ptypes.DurationProto(2 * time.Second),
				UpThreshold:   2,
				DownThreshold: 1,
			},
		}

		str := server.PrettyString()
		Expect(str).ToNot(BeEmpty())
		fmt.Println(str)
	})
})
