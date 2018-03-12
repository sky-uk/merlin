package healthchecks

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/wrappers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/sky-uk/merlin/types"
)

func TestHealthChecker(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Health Checks Suite")
}

var (
	// guarded by serverLock
	serverStatus int
	serverLock   sync.Mutex
)

func setServerStatus(code int) {
	serverLock.Lock()
	defer serverLock.Unlock()
	serverStatus = code
}

func getServerStatus() int {
	serverLock.Lock()
	defer serverLock.Unlock()
	return serverStatus
}

var _ = Describe("HealthChecks", func() {
	var (
		serviceID                               = "myservice"
		checkPath                               = "/health"
		check                                   *types.RealServer_HealthCheck
		period, timeout, waitForUp, waitForDown time.Duration
		checker                                 Checker
		ts                                      *httptest.Server
		localServer1                            = &types.RealServer_Key{Ip: "127.0.0.1"}
		localServer2                            = &types.RealServer_Key{Ip: "localhost"}
	)

	BeforeEach(func() {
		setServerStatus(http.StatusOK)
		checker = New()
		period = 50 * time.Millisecond
		timeout = 20 * time.Millisecond
	})

	JustBeforeEach(func() {
		ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != checkPath {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(getServerStatus())
			fmt.Fprintln(w, "kaboom!")
		}))

		u, _ := url.Parse(ts.URL)
		check = &types.RealServer_HealthCheck{
			Endpoint:      &wrappers.StringValue{Value: fmt.Sprintf("http://:%s%s", u.Port(), checkPath)},
			Period:        ptypes.DurationProto(period),
			Timeout:       ptypes.DurationProto(timeout),
			UpThreshold:   4,
			DownThreshold: 2,
		}
		waitForUp = time.Duration(check.UpThreshold) * (timeout + period)
		waitForDown = time.Duration(check.DownThreshold) * (timeout + period)
	})

	AfterEach(func() {
		checker.Stop()
		ts.Close()
	})

	It("should report server is down if non-responsive", func() {
		checker := New()
		check.Endpoint = &wrappers.StringValue{Value: "http://:9999/nowhere"}
		checker.SetHealthCheck(serviceID, localServer1, check)

		time.Sleep(waitForDown)
		Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
	})

	DescribeTable("validate health check", func(endpoint string) {
		checker := New()
		check.Endpoint = &wrappers.StringValue{Value: endpoint}
		err := checker.SetHealthCheck(serviceID, localServer1, check)
		Expect(err).To(HaveOccurred())
	},
		Entry("unsupported scheme", "myscheme://:9111/health"),
		Entry("garbage", "asdf"))

	Context("server is up", func() {
		BeforeEach(func() {
			setServerStatus(http.StatusOK)
		})

		It("should not list servers in down IPs", func() {
			checker.SetHealthCheck(serviceID, localServer1, check)
			checker.SetHealthCheck(serviceID, localServer2, check)

			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
			Expect(checker.IsDown(serviceID, localServer2)).To(BeFalse())
		})

		It("should consider server down before health check happens", func() {
			checker.SetHealthCheck(serviceID, localServer1, check)

			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
		})

		It("can transition to down and back up", func() {
			checker.SetHealthCheck(serviceID, localServer1, check)
			time.Sleep(waitForUp)

			setServerStatus(http.StatusInternalServerError)
			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())

			setServerStatus(http.StatusOK)
			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
		})

		It("can update to a passing health check", func() {
			brokenCheck := proto.Clone(check).(*types.RealServer_HealthCheck)
			brokenCheck.Endpoint = &wrappers.StringValue{Value: "http://:9999/nowhere"}
			checker.SetHealthCheck(serviceID, localServer1, brokenCheck)

			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())

			checker.SetHealthCheck(serviceID, localServer1, check)
			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
		})

		It("can remove then add back the server", func() {
			checker.SetHealthCheck(serviceID, localServer1, check)
			checker.RemHealthCheck(serviceID, localServer1)
			time.Sleep(waitForUp)
			checker.RemHealthCheck(serviceID, localServer1)
			checker.SetHealthCheck(serviceID, localServer1, check)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
		})

		It("should keep the server state if we set the same health check", func() {
			checker.SetHealthCheck(serviceID, localServer1, check)

			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())

			checker.SetHealthCheck(serviceID, localServer1, check)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
		})

		It("should be able to update the health check", func() {
			checker.SetHealthCheck(serviceID, localServer1, check)

			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())

			check2 := proto.Clone(check).(*types.RealServer_HealthCheck)
			check2.DownThreshold = 1
			check2.Endpoint = &wrappers.StringValue{Value: "http://localhost:99999/nowhere"}
			checker.SetHealthCheck(serviceID, localServer1, check2)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse(), "nothing should change immediately after setting check")

			// wait one period, since down threshold is 1 now
			time.Sleep(period + timeout)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
		})
	})

	Context("server is down", func() {
		BeforeEach(func() {
			setServerStatus(http.StatusInternalServerError)
		})

		It("should list server in down IPs", func() {
			checker.SetHealthCheck(serviceID, localServer1, check)
			checker.SetHealthCheck(serviceID, localServer2, check)

			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
			Expect(checker.IsDown(serviceID, localServer2)).To(BeTrue())
		})

		It("can transition to up and back down", func() {
			checker.SetHealthCheck(serviceID, localServer1, check)
			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())

			setServerStatus(http.StatusOK)
			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())

			setServerStatus(http.StatusInternalServerError)
			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
		})

		It("should no longer report down after disabling health check", func() {
			checker.SetHealthCheck(serviceID, localServer1, check)
			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())

			By("removing the health check", func() {
				checker.SetHealthCheck(serviceID, localServer1, &types.RealServer_HealthCheck{})
				Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
			})

			By("re-adding the health check", func() {
				checker.SetHealthCheck(serviceID, localServer1, check)
				Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
			})
		})
	})
})
