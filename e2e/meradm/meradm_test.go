package Meradm

import (
	"testing"

	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"net/http"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/sky-uk/merlin/e2e"
)

func TestE2EMeradm(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Meradm Suite")
}

var _ = Describe("Meradm", func() {
	BeforeSuite(func() {
		SetupE2E()
	})

	BeforeEach(func() {
		StartEtcd()
		StartMerlin()
	})

	AfterEach(func() {
		StopMerlin()
		StopEtcd()
	})

	Describe("services", func() {
		Context("add service", func() {
			It("succeeds", func() {
				meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")

				out := meradmList()

				Expect(out).To(ContainElement(MatchRegexp(`.*service1.*TCP.*10.1.1.1:888.*wrr.*flag-1,flag-2.*`)))
			})

			It("syncs the reconciler", func() {
				// flush stderr so far
				MerlinStderr()
				meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
				time.Sleep(50 * time.Millisecond)
				Expect(MerlinStderr()).To(ContainElement(ContainSubstring("stub-reconciler: Sync()")))
			})

			It("succeeds without scheduler flags", func() {
				meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr")

				out := meradmList()

				Expect(out).To(ContainElement(MatchRegexp(`.*service1.*TCP.*10.1.1.1:888.*wrr.*`)))
			})

			It("fails if scheduler is not specified", func() {
				_, err := meradmErrored("service", "add", "service1", "tcp", "10.1.1.1:888")
				Expect(err).To(HaveOccurred())
			})
		})

		It("can edit an existing service", func() {
			meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
			meradm("service", "edit", "service1", "-s=sh", "-b=flag-3")

			out := meradmList()

			Expect(out).To(ContainElement(MatchRegexp(`.*service1.*TCP.*10.1.1.1:888.*sh.*flag-3.*`)))
		})

		It("can delete an existing service", func() {
			meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
			meradm("service", "del", "service1")

			out := meradmList()

			Expect(out).NotTo(ContainElement(MatchRegexp(`.*service1.*`)))
		})

		It("fails if required fields are unset", func() {
			_, err := meradmErrored("service", "add", "service1", "tcp")
			Expect(err).To(HaveOccurred(), "should have failed validation")
			_, err = meradmErrored("service", "add", "service1")
			Expect(err).To(HaveOccurred(), "should have failed validation")
		})
	})

	Describe("servers", func() {
		BeforeEach(func() {
			meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
		})

		Context("add a server", func() {
			It("succeeds", func() {
				meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq",
					"--health-endpoint=http://:556/health", "--health-period=5s", "--health-timeout=1s",
					"--health-up=2", "--health-down=1")

				out := meradmList()

				Expect(out).To(ContainElement(MatchRegexp(`.*172.16.1.1:555.*MASQ.*2.*`)))
				Expect(out).To(ContainElement(MatchRegexp(`http://:556/health.*5s.*1s.*2/1`)))
			})

			It("succeeds without health check provided", func() {
				meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq")

				out := meradmList()

				Expect(out).To(ContainElement(MatchRegexp(`.*172.16.1.1:555.*MASQ.*2.*`)))
			})

			It("syncs the reconciler", func() {
				// flush stderr so far
				MerlinStderr()
				meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq")
				time.Sleep(50 * time.Millisecond)
				Expect(MerlinStderr()).To(ContainElement(ContainSubstring("stub-reconciler: Sync()")))
			})
		})

		It("can edit a server", func() {
			meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq",
				"--health-endpoint=http://:556/health", "--health-period=5s", "--health-timeout=1s",
				"--health-up=2", "--health-down=1")
			meradm("server", "edit", "service1", "172.16.1.1:555", "-w=5")
			meradm("server", "edit", "service1", "172.16.1.1:555", "-f=route")
			meradm("server", "edit", "service1", "172.16.1.1:555", "--health-period=10s")

			out := meradmList()

			Expect(out).To(ContainElement(MatchRegexp(`.*172.16.1.1:555.*ROUTE.*5.*`)))
			Expect(out).ToNot(ContainElement(MatchRegexp(`.*MASQ.*`)))
			Expect(out).ToNot(ContainElement(MatchRegexp(`.* 2 .*`)))
			Expect(out).To(ContainElement(MatchRegexp(`http://:556/health.*10s.*1s.*2/1`)))
		})

		It("can delete a server", func() {
			meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq")
			meradm("server", "del", "service1", "172.16.1.1:555")

			out := meradmList()

			Expect(out).ToNot(ContainElement(MatchRegexp(`.*172.16.1.1.*`)))
		})

		It("can optionally set flags", func() {
			meradm("server", "add", "service1", "172.16.1.1:555", "-f=masq", "-w=1")
			meradm("server", "edit", "service1", "172.16.1.1:555", "-f=route")
			meradm("server", "edit", "service1", "172.16.1.1:555", "-w=4")

			out := meradmList()

			Expect(out).To(ContainElement(MatchRegexp(`.*172.16.1.1:555.*ROUTE.*4.*`)))
		})

		It("requires flags are set when adding a service", func() {
			_, err := meradmErrored("server", "add", "service1", "172.16.1.1:555")
			Expect(err).To(HaveOccurred())
		})

		It("can disable a healthcheck", func() {
			meradm("server", "add", "service1", "172.16.1.1:555", "-f=masq", "-w=1",
				"--health-endpoint=http://:556/health", "--health-period=5s", "--health-timeout=1s",
				"--health-up=2", "--health-down=1")
			meradm("server", "edit", "service1", "172.16.1.1:555", "--health-endpoint=")

			out := meradmList()

			Expect(out).ToNot(ContainElement(ContainSubstring("http://:556/health")))
		})
	})

	Describe("External Updates", func() {
		It("should run reconciler if etcd is updated elsewhere", func() {
			meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
			meradm("server", "add", "service1", "172.16.1.1:555", "-w=1", "-f=masq")
			time.Sleep(100 * time.Millisecond)
			MerlinStderr()

			etcdDel := func(path string) {
				serverURL := fmt.Sprintf("http://localhost:%s/v2/keys/merlin/%s", EtcdPort(), path)
				req, _ := http.NewRequest(http.MethodDelete, serverURL, nil)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					panic(err)
				}
				defer resp.Body.Close()
				Expect(resp.StatusCode).To(Equal(http.StatusOK))
			}

			// delete server directly in etcd
			etcdDel("servers/service1/172.16.1.1:555")
			time.Sleep(250 * time.Millisecond)
			stderr := MerlinStderr()
			Expect(stderr).To(ContainElement(ContainSubstring("stub-reconciler: Sync()")))

			// delete service directly in etcd
			etcdDel("services/service1")
			time.Sleep(250 * time.Millisecond)
			stderr = MerlinStderr()
			Expect(stderr).To(ContainElement(ContainSubstring("stub-reconciler: Sync()")))
		})
	})
})

func meradmList() []string {
	return strings.Split(strings.TrimSpace(meradm("list")), "\n")
}

func meradm(args ...string) string {
	out, err := meradmErrored(args...)
	Expect(err).ToNot(HaveOccurred())
	return out
}

func meradmErrored(args ...string) (string, error) {
	args = append(args, "-H=localhost", "-P="+MerlinPort())
	c := exec.Command("meradm", args...)
	c.Stderr = os.Stderr
	var output bytes.Buffer
	c.Stdout = &output
	fmt.Printf("->\n%v\n", c.Args)
	err := c.Run()
	out := output.String()
	fmt.Printf("<-\n%s(%v)\n", out, err)
	return out, err
}
