// Package e2e contains end to end tests with a mocked IPVS so it can run in build environments.
// The main purpose is to test the client/server -> store CRUD functionality.
// Reconciler and IPVS logic is tested in reconciler/reconciler_test.go.
package e2e

import (
	"os"
	"testing"

	"io/ioutil"

	"os/exec"

	"bytes"
	"strings"

	"fmt"

	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	etcdDownloadURL    = "https://github.com/coreos/etcd/releases/download/v2.3.8/etcd-v2.3.8-linux-amd64.tar.gz"
	etcdExpandedPath   = "etcd-v2.3.8-linux-amd64/"
	etcdDownloadSha256 = "3431112f97105733aabb95816df99e44beabed2cc96201679e3b3b830ac35282"
	buildDir           = "../build"
	etcdBinary         = buildDir + "/etcd"
	etcdTarball        = buildDir + "/etcd.tar.gz"
)

var (
	etcdListenPort   string
	etcdPeerPort     string
	merlinPort       string
	merlinHealthPort string
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = Describe("E2E", func() {
	var (
		dataDir string
		etcd    *exec.Cmd
		merlin  *exec.Cmd
	)

	BeforeSuite(func() {
		if err := os.Mkdir(buildDir, 0755); err != nil && !os.IsExist(err) {
			panic(err)
		}
		if _, err := os.Stat(etcdBinary); os.IsNotExist(err) {
			downloadEtcdBinary()
		}
	})

	AfterSuite(func() {
		stopServer(merlin)
	})

	BeforeEach(func() {
		// etcd
		var err error
		dataDir, err = ioutil.TempDir(buildDir, "")
		if err != nil {
			panic(err)
		}

		ports := findFreePorts(2)
		etcdListenPort = strconv.Itoa(ports[0])
		etcdPeerPort = strconv.Itoa(ports[1])

		etcd = startEtcd(dataDir, etcdListenPort, etcdPeerPort)
		waitForServerToStart("etcd", etcdListenPort, "/health")

		// merlin
		ports = findFreePorts(2)
		merlinPort = strconv.Itoa(ports[0])
		merlinHealthPort = strconv.Itoa(ports[1])

		merlin = exec.Command("merlin",
			"-port="+merlinPort,
			"-health-port="+merlinHealthPort,
			"-store-endpoints=http://127.0.0.1:"+etcdListenPort,
			"-reconcile=false")
		merlin.Stdout = os.Stdout
		merlin.Stderr = os.Stderr
		if err := merlin.Start(); err != nil {
			Fail(err.Error())
		}
		waitForServerToStart("merlin", merlinHealthPort, "/health")
	})

	AfterEach(func() {
		stopServer(etcd)
		os.RemoveAll(dataDir)
		stopServer(merlin)
	})

	Describe("services", func() {
		It("can add a new service", func() {
			meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")

			out := strings.Split(meradm("list"), "\n")

			Expect(out).To(ContainElement(MatchRegexp(`.*service1.*TCP.*10.1.1.1:888.*wrr.*flag-1,flag-2.*`)))
		})

		It("can edit an existing service", func() {
			meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
			meradm("service", "edit", "service1", "-s=sh", "-b=flag-3")

			out := strings.Split(meradm("list"), "\n")

			Expect(out).To(ContainElement(MatchRegexp(`.*service1.*TCP.*10.1.1.1:888.*sh.*flag-3.*`)))
		})

		It("can delete an existing service", func() {
			meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
			meradm("service", "del", "service1")

			out := strings.Split(meradm("list"), "\n")

			Expect(out).NotTo(ContainElement(MatchRegexp(`.*service1.*`)))
		})

		It("fails if required fields are unset", func() {
			_, err := meradm2("service", "add", "service1", "tcp")
			Expect(err).To(HaveOccurred(), "should have failed validation")
			_, err = meradm2("service", "add", "service1")
			Expect(err).To(HaveOccurred(), "should have failed validation")
		})
	})
})

func meradm(args ...string) string {
	out, err := meradm2(args...)
	Expect(err).ToNot(HaveOccurred())
	return out
}

func meradm2(args ...string) (string, error) {
	args = append(args, "-H=localhost", "-P="+merlinPort)
	c := exec.Command("meradm", args...)
	c.Stderr = os.Stderr
	var output bytes.Buffer
	c.Stdout = &output
	fmt.Printf("-> %v\n", c.Args)
	err := c.Run()
	out := output.String()
	fmt.Printf("<- %s(%v)\n", out, err)
	return out, err
}
