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
		id                                      = "myservice"
		checkPath                               = "/health"
		check                                   *types.VirtualService_HealthCheck
		period, timeout, waitForUp, waitForDown time.Duration
		checker                                 Checker
		ts                                      *httptest.Server
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
		check = &types.VirtualService_HealthCheck{
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
		checker.SetHealthCheck(id, check)
		checker.AddServer(id, "127.0.0.1")

		time.Sleep(waitForDown)
		downServers := checker.GetDownServers(id)

		Expect(downServers).To(ConsistOf("127.0.0.1"))
	})

	DescribeTable("validate health check", func(endpoint string) {
		checker := New()
		check.Endpoint = &wrappers.StringValue{Value: endpoint}
		err := checker.SetHealthCheck(id, check)
		Expect(err).To(HaveOccurred())
	},
		Entry("unsupported scheme", "myscheme://:9111/health"),
		Entry("garbage", "asdf"))

	Context("server is up", func() {
		BeforeEach(func() {
			setServerStatus(http.StatusOK)
		})

		It("should not list servers in down IPs", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			checker.AddServer(id, "localhost")

			time.Sleep(waitForUp)
			downServers := checker.GetDownServers(id)

			Expect(downServers).To(BeEmpty())
		})

		It("should consider server down before health check happens", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")

			downServers := checker.GetDownServers(id)

			Expect(downServers).To(ConsistOf("127.0.0.1"))
		})

		It("can transition to down and back up", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			time.Sleep(waitForUp)

			setServerStatus(http.StatusInternalServerError)
			time.Sleep(waitForDown)
			Expect(checker.GetDownServers(id)).To(ConsistOf("127.0.0.1"))

			setServerStatus(http.StatusOK)
			time.Sleep(waitForUp)
			Expect(checker.GetDownServers(id)).To(BeEmpty())
		})

		It("should update health check, finding server", func() {
			brokenCheck := proto.Clone(check).(*types.VirtualService_HealthCheck)
			brokenCheck.Endpoint = &wrappers.StringValue{Value: "http://:9999/nowhere"}
			checker.SetHealthCheck(id, brokenCheck)
			checker.AddServer(id, "127.0.0.1")

			time.Sleep(waitForUp)
			downServers := checker.GetDownServers(id)
			Expect(downServers).To(ConsistOf("127.0.0.1"))

			checker.SetHealthCheck(id, check)
			time.Sleep(waitForUp)
			downServers = checker.GetDownServers(id)
			Expect(downServers).To(BeEmpty())

			// check that adding a server after health check change also works
			checker.AddServer(id, "localhost")
			time.Sleep(waitForUp)
			downServers = checker.GetDownServers(id)
			Expect(downServers).To(BeEmpty())
		})

		It("can add health check after adding servers", func() {
			checker.RemServer(id, "127.0.0.1")
			checker.AddServer(id, "127.0.0.1")
			checker.RemServer(id, "127.0.0.1")
			checker.AddServer(id, "127.0.0.1")
			checker.AddServer(id, "localhost")
			checker.SetHealthCheck(id, check)

			time.Sleep(waitForUp)
			downServers := checker.GetDownServers(id)

			Expect(downServers).To(BeEmpty())

			// changing health check to broken should work as well
			brokenCheck := proto.Clone(check).(*types.VirtualService_HealthCheck)
			brokenCheck.Endpoint = &wrappers.StringValue{Value: "http://:9999/nowhere"}
			checker.SetHealthCheck(id, brokenCheck)
			time.Sleep(waitForDown)
			Expect(checker.GetDownServers(id)).To(ConsistOf("127.0.0.1", "localhost"))
		})

		It("can remove then add back the server", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			checker.RemServer(id, "127.0.0.1")
			time.Sleep(waitForUp)
			Expect(checker.GetDownServers(id)).To(BeEmpty())
			checker.RemServer(id, "127.0.0.1")
			Expect(checker.GetDownServers(id)).To(BeEmpty())
			checker.AddServer(id, "127.0.0.1")
			Expect(checker.GetDownServers(id)).To(ConsistOf("127.0.0.1"))
			time.Sleep(waitForUp)
			Expect(checker.GetDownServers(id)).To(BeEmpty())
		})

		It("should keep the server state if we set the same health check", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			checker.AddServer(id, "localhost")

			time.Sleep(waitForUp)
			downServers := checker.GetDownServers(id)
			Expect(downServers).To(BeEmpty())

			checker.SetHealthCheck(id, check)
			Expect(checker.GetDownServers(id)).To(BeEmpty())
		})

		It("should be able to update the health check", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			checker.AddServer(id, "localhost")

			time.Sleep(waitForUp)
			downServers := checker.GetDownServers(id)
			Expect(downServers).To(BeEmpty())

			check2 := proto.Clone(check).(*types.VirtualService_HealthCheck)
			check2.DownThreshold = 1
			check2.Endpoint = &wrappers.StringValue{Value: "http://localhost:99999/nowhere"}
			checker.SetHealthCheck(id, check2)
			Expect(checker.GetDownServers(id)).To(BeEmpty(), "nothing should change immediately after setting check")

			// wait one period, since down threshold is 1 now
			time.Sleep(period + timeout)
			Expect(checker.GetDownServers(id)).To(ConsistOf("127.0.0.1", "localhost"))
		})
	})

	Context("server is down", func() {
		BeforeEach(func() {
			setServerStatus(http.StatusInternalServerError)
		})

		It("should list server in down IPs", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "localhost")
			checker.AddServer(id, "127.0.0.1")

			time.Sleep(waitForDown)
			downServers := checker.GetDownServers(id)

			Expect(downServers).To(ConsistOf("localhost", "127.0.0.1"))
		})

		It("should not list server as down if removed", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")

			time.Sleep(waitForDown)
			checker.RemServer(id, "127.0.0.1")
			time.Sleep(waitForDown)
			downServers := checker.GetDownServers(id)

			Expect(downServers).To(BeEmpty())
		})

		It("can transition to up and back down", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			time.Sleep(waitForDown)

			setServerStatus(http.StatusOK)
			time.Sleep(waitForUp)
			Expect(checker.GetDownServers(id)).To(BeEmpty())

			setServerStatus(http.StatusInternalServerError)
			time.Sleep(waitForDown)
			Expect(checker.GetDownServers(id)).To(ConsistOf("127.0.0.1"))
		})

		It("should no longer report down after removing health check", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			time.Sleep(waitForDown)
			Expect(checker.GetDownServers(id)).To(ConsistOf("127.0.0.1"))

			By("removing the health check", func() {
				checker.SetHealthCheck(id, &types.VirtualService_HealthCheck{})
				Expect(checker.GetDownServers(id)).To(BeEmpty())
			})

			By("adding another server", func() {
				checker.AddServer(id, "localhost")
				time.Sleep(waitForDown)
				Expect(checker.GetDownServers(id)).To(BeEmpty())
			})

			By("removing a server", func() {
				checker.RemServer(id, "127.0.0.1")
				time.Sleep(waitForDown)
				Expect(checker.GetDownServers(id)).To(BeEmpty())
			})

			By("re-adding the health check", func() {
				checker.SetHealthCheck(id, check)
				time.Sleep(waitForDown)
				Expect(checker.GetDownServers(id)).To(ConsistOf("localhost"))
			})
		})
	})
})
