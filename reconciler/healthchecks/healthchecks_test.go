package healthchecks

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sync"

	"fmt"
	"net/url"

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

func stubTransitionFn(_ HealthState) {}

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

	It("should report server is down if non-responsive", func(done Done) {
		checker := New()
		check.Endpoint = &wrappers.StringValue{Value: "http://:9999/nowhere"}
		checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)

		time.Sleep(waitForDown)
		Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
		close(done)
	}, 1.0)

	DescribeTable("validate health check", func(endpoint string) {
		checker := New()
		check.Endpoint = &wrappers.StringValue{Value: endpoint}
		err := checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
		Expect(err).To(HaveOccurred())
	},
		Entry("unsupported scheme", "myscheme://:9111/health"),
		Entry("garbage", "asdf"))

	Context("server is up", func() {
		BeforeEach(func() {
			setServerStatus(http.StatusOK)
		})

		It("should not list servers in down IPs", func(done Done) {
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
			checker.SetHealthCheck(serviceID, localServer2, check, stubTransitionFn)

			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
			Expect(checker.IsDown(serviceID, localServer2)).To(BeFalse())
			close(done)
		}, 1.0)

		It("should consider server down before health check happens", func(done Done) {
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)

			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
			close(done)
		}, 1.0)

		It("can transition to down and back up", func(done Done) {
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
			time.Sleep(waitForUp)

			setServerStatus(http.StatusInternalServerError)
			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())

			setServerStatus(http.StatusOK)
			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
			close(done)
		})

		It("can update to a passing health check", func(done Done) {
			brokenCheck := proto.Clone(check).(*types.RealServer_HealthCheck)
			brokenCheck.Endpoint = &wrappers.StringValue{Value: "http://:9999/nowhere"}
			checker.SetHealthCheck(serviceID, localServer1, brokenCheck, stubTransitionFn)

			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())

			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
			close(done)
		}, 1.0)

		It("can remove then add back the server", func(done Done) {
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
			checker.RemHealthCheck(serviceID, localServer1)
			time.Sleep(waitForUp)
			checker.RemHealthCheck(serviceID, localServer1)
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
			close(done)
		}, 1.0)

		It("should keep the server state if we set the same health check", func(done Done) {
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)

			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())

			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
			close(done)
		}, 1.0)

		It("should be able to update the health check", func(done Done) {
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)

			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())

			check2 := proto.Clone(check).(*types.RealServer_HealthCheck)
			check2.DownThreshold = 1
			check2.Endpoint = &wrappers.StringValue{Value: "http://localhost:99999/nowhere"}
			checker.SetHealthCheck(serviceID, localServer1, check2, stubTransitionFn)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse(), "nothing should change immediately after setting check")

			// wait one period, since down threshold is 1 now
			time.Sleep(period + timeout)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
			close(done)
		}, 1.0)
	})

	Context("server is down", func() {
		BeforeEach(func() {
			setServerStatus(http.StatusInternalServerError)
		})

		It("should list server in down IPs", func(done Done) {
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
			checker.SetHealthCheck(serviceID, localServer2, check, stubTransitionFn)

			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
			Expect(checker.IsDown(serviceID, localServer2)).To(BeTrue())
			close(done)
		}, 1.0)

		It("can transition to up and back down", func(done Done) {
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())

			setServerStatus(http.StatusOK)
			time.Sleep(waitForUp)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())

			setServerStatus(http.StatusInternalServerError)
			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
			close(done)
		}, 1.0)

		It("should no longer report down after disabling health check", func(done Done) {
			checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
			time.Sleep(waitForDown)
			Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())

			By("removing the health check", func() {
				checker.SetHealthCheck(serviceID, localServer1, &types.RealServer_HealthCheck{}, stubTransitionFn)
				Expect(checker.IsDown(serviceID, localServer1)).To(BeFalse())
			})

			By("re-adding the health check", func() {
				checker.SetHealthCheck(serviceID, localServer1, check, stubTransitionFn)
				Expect(checker.IsDown(serviceID, localServer1)).To(BeTrue())
			})
			close(done)
		}, 1.0)
	})

	Context("transition function", func() {
		var (
			called        bool
			receivedState HealthState
			lock          *sync.Mutex
		)

		BeforeEach(func() {
			called = false
			receivedState = ServerDown
			lock = &sync.Mutex{}
			checker.SetHealthCheck(serviceID, localServer1, check, func(state HealthState) {
				lock.Lock()
				defer lock.Unlock()
				called = true
				receivedState = state
			})
		})

		It("should be invoked when transitioning from up to down", func() {
			setServerStatus(http.StatusOK)

			time.Sleep(waitForUp)
			func() {
				lock.Lock()
				defer lock.Unlock()
				Expect(called).To(BeTrue())
				Expect(receivedState).To(Equal(ServerUp))
				called = false
			}()

			setServerStatus(http.StatusInternalServerError)
			time.Sleep(waitForDown)
			func() {
				lock.Lock()
				defer lock.Unlock()
				Expect(called).To(BeTrue())
				Expect(receivedState).To(Equal(ServerDown))
			}()
		})

		It("should be invoked when transitioning from down to up", func() {
			setServerStatus(http.StatusInternalServerError)

			time.Sleep(waitForDown)
			func() {
				lock.Lock()
				defer lock.Unlock()
				Expect(called).To(BeFalse())
			}()

			setServerStatus(http.StatusOK)
			time.Sleep(waitForUp)
			func() {
				lock.Lock()
				defer lock.Unlock()
				Expect(called).To(BeTrue())
				Expect(receivedState).To(Equal(ServerUp))
			}()
		})

		It("can be updated", func() {
			setServerStatus(http.StatusOK)
			var updatedCalled bool

			checker.SetHealthCheck(serviceID, localServer1, check, func(_ HealthState) {
				lock.Lock()
				defer lock.Unlock()
				updatedCalled = true
			})

			time.Sleep(waitForUp)
			func() {
				lock.Lock()
				defer lock.Unlock()
				Expect(called).To(BeFalse(), "old transition func should not be called")
				Expect(updatedCalled).To(BeTrue(), "new transition func should be called")
			}()
		})

		It("nil transition func can be used", func() {
			setServerStatus(http.StatusOK)
			checker.SetHealthCheck(serviceID, localServer1, check, nil)
			time.Sleep(waitForUp)
			func() {
				lock.Lock()
				defer lock.Unlock()
				Expect(called).To(BeFalse(), "old transition func should not be called")
			}()
		})
	})
})
