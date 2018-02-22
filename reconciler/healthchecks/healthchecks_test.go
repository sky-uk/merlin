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
		id                     = "myservice"
		checkPath              = "/health"
		check                  *types.VirtualService_HealthCheck
		waitForUp, waitForDown time.Duration
		checker                Checker
		ts                     *httptest.Server
	)

	BeforeEach(func() {
		setServerStatus(http.StatusOK)
		checker = New()
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
			Endpoint:      fmt.Sprintf("http://:%s%s", u.Port(), checkPath),
			Period:        ptypes.DurationProto(50 * time.Millisecond),
			Timeout:       ptypes.DurationProto(20 * time.Millisecond),
			UpThreshold:   3,
			DownThreshold: 2,
		}
		waitForUp = time.Duration(check.UpThreshold) * 150 * time.Millisecond
		waitForDown = time.Duration(check.DownThreshold) * 150 * time.Millisecond
	})

	AfterEach(func() {
		checker.Stop()
		ts.Close()
	})

	It("should report server is down if non-responsive", func() {
		checker := New()
		check.Endpoint = "http://:9999/nowhere"
		checker.SetHealthCheck(id, check)
		checker.AddServer(id, "127.0.0.1")

		time.Sleep(waitForDown)
		downServers := checker.GetDownServers(id)

		Expect(downServers).To(ConsistOf("127.0.0.1"))
	})

	DescribeTable("validate health check", func(endpoint string) {
		checker := New()
		check.Endpoint = endpoint
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

		It("can transition to down", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			time.Sleep(waitForUp)

			setServerStatus(http.StatusInternalServerError)
			time.Sleep(waitForDown)
			downServers := checker.GetDownServers(id)

			Expect(downServers).To(ConsistOf("127.0.0.1"))
		})

		It("should update health check, finding server", func() {
			brokenCheck := proto.Clone(check).(*types.VirtualService_HealthCheck)
			brokenCheck.Endpoint = "http://:9999/nowhere"
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
			brokenCheck.Endpoint = "http://:9999/nowhere"
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
	})

	Context("server is down", func() {
		BeforeEach(func() {
			setServerStatus(http.StatusInternalServerError)
		})

		It("should list server in down IPs", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")

			time.Sleep(waitForDown)
			downServers := checker.GetDownServers(id)

			Expect(downServers).To(ConsistOf("127.0.0.1"))
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

		It("can transition to up", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			time.Sleep(waitForDown)

			setServerStatus(http.StatusOK)
			time.Sleep(waitForUp)
			downServers := checker.GetDownServers(id)

			Expect(downServers).To(BeEmpty())
		})

		It("should no longer report down after removing health check", func() {
			checker.SetHealthCheck(id, check)
			checker.AddServer(id, "127.0.0.1")
			time.Sleep(waitForDown)
			Expect(checker.GetDownServers(id)).To(ConsistOf("127.0.0.1"))

			By("removing the health check", func() {
				checker.SetHealthCheck(id, nil)
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