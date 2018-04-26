// Package e2e sets up end to end tests with a stubbed IPVS so it can run in build environments.
// The main purpose is to test the client/server -> store CRUD functionality.
// The actual specs are located in api/ and meradm/.
package e2e

import (
	"os"

	"io/ioutil"

	"os/exec"

	"fmt"

	"strconv"

	"bytes"
	"crypto/sha256"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	. "github.com/onsi/gomega"
)

const (
	etcdDownloadURL    = "https://github.com/coreos/etcd/releases/download/v2.3.8/etcd-v2.3.8-linux-amd64.tar.gz"
	etcdExpandedPath   = "etcd-v2.3.8-linux-amd64/"
	etcdDownloadSha256 = "3431112f97105733aabb95816df99e44beabed2cc96201679e3b3b830ac35282"
	// assumes all specs run in subfolders of e2e
	workingDir = "../.."
	buildDir   = workingDir + "/build"
	etcdBinary = buildDir + "/etcd"
)

var (
	etcdListenPort   string
	etcdPeerPort     string
	merlinPort       string
	merlinHealthPort string
	dataDir          string
	etcd             *exec.Cmd
	merlin           *exec.Cmd
	merlinStdout     safeBuffer
	merlinStderr     safeBuffer
)

func SetupE2E() {
	if err := os.Mkdir(buildDir, 0755); err != nil && !os.IsExist(err) {
		panic(err)
	}
	if _, err := os.Stat(etcdBinary); os.IsNotExist(err) {
		downloadEtcdBinary()
	}
	cmd := exec.Command("make", "install")
	cmd.Dir = workingDir
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Println(string(out))
		panic(err)
	}
}

func downloadEtcdBinary() {
	fmt.Fprintln(os.Stderr, "Downloading etcd binary from "+etcdDownloadURL)
	tarball := "etcd.tar.gz"

	// download to a temporary directory to help avoid any issues around concurrent runs.
	tmpDir, err := ioutil.TempDir(buildDir, "dl")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)
	tmpTarball := tmpDir + "/" + tarball

	// Download tarball.
	func() {
		out, err := os.Create(tmpTarball)
		if err != nil {
			panic(err)
		}
		defer out.Close()

		resp, err := http.Get(etcdDownloadURL)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()

		if _, err = io.Copy(out, resp.Body); err != nil {
			panic(err)
		}
	}()

	// Checksum tarball.
	func() {
		fmt.Fprintln(os.Stderr, "Verifying checksum")
		f, err := os.Open(tmpTarball)
		if err != nil {
			panic(err)
		}
		defer f.Close()

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			panic(err)
		}
		hs := fmt.Sprintf("%x", h.Sum(nil))
		if hs != etcdDownloadSha256 {
			panic("invalid sha256 sum on etcd tarball " + tmpTarball)
		}
	}()

	// Expand tarball.
	fmt.Fprintln(os.Stderr, "Expanding etcd binary into "+buildDir)
	c := exec.Command("/bin/sh", "-c", "tar xzvf "+tarball+" && mv "+etcdExpandedPath+"etcd* "+buildDir+"/")
	c.Dir = tmpDir
	if out, err := c.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, string(out))
		panic(err)
	}
}

func EtcdPort() string {
	return etcdListenPort
}

func StartEtcd() {
	var err error
	dataDir, err = ioutil.TempDir(buildDir, "")
	if err != nil {
		panic(err)
	}

	ports := findFreePorts(2)
	etcdListenPort = strconv.Itoa(ports[0])
	etcdPeerPort = strconv.Itoa(ports[1])

	etcd = exec.Command(etcdBinary,
		"-name=etcd0",
		"-data-dir="+dataDir,
		"-advertise-client-urls=http://127.0.0.1:"+etcdListenPort,
		"-listen-client-urls=http://0.0.0.0:"+etcdListenPort,
		"-initial-advertise-peer-urls=http://127.0.0.1:"+etcdPeerPort,
		"-listen-peer-urls=http://0.0.0.0:"+etcdPeerPort,
		"-initial-cluster-token=etcd-cluster-1",
		"-initial-cluster=etcd0=http://127.0.0.1:"+etcdPeerPort,
		"-initial-cluster-state=new")
	etcd.Stdout = os.Stdout
	etcd.Stderr = os.Stderr
	if err := etcd.Start(); err != nil {
		panic(err)
	}
	waitForServerToStart("etcd", etcdListenPort, "/health")
}

func StopEtcd() {
	stopServer(etcd, false)
	os.RemoveAll(dataDir)
}

func MerlinPort() string {
	return merlinPort
}

// MerlinStdout is the stdout of the merlin process. Subsequent calls only return new output.
func MerlinStdout() []string {
	s, _ := ioutil.ReadAll(&merlinStdout)
	return strings.Split(string(s), "\n")
}

// MerlinStderr is the stderr of the merlin process. Subsequent calls only return new output.
func MerlinStderr() []string {
	s, _ := ioutil.ReadAll(&merlinStderr)
	return strings.Split(string(s), "\n")
}

func StartMerlin() {
	ports := findFreePorts(2)
	merlinPort = strconv.Itoa(ports[0])
	merlinHealthPort = strconv.Itoa(ports[1])

	merlin = exec.Command("merlin",
		"-port="+merlinPort,
		"-health-port="+merlinHealthPort,
		"-store-endpoints=http://127.0.0.1:"+etcdListenPort,
		"-reconcile=false",
		"-debug")

	// wire up pipes so we can save and assert on output, while preserving stderr/stdout
	prOut, pwOut := io.Pipe()
	prErr, pwErr := io.Pipe()
	teeOut := io.TeeReader(prOut, os.Stdout)
	teeErr := io.TeeReader(prErr, os.Stderr)
	go func() {
		io.Copy(&merlinStdout, teeOut)
	}()
	go func() {
		io.Copy(&merlinStderr, teeErr)
	}()

	merlin.Stdout = pwOut
	merlin.Stderr = pwErr
	if err := merlin.Start(); err != nil {
		panic(err.Error())
	}
	waitForServerToStart("merlin", merlinHealthPort, "/health")
}

func StopMerlin() {
	stopServer(merlin, true)
}

func waitForServerToStart(name, port, healthPath string) {
	fmt.Fprintf(os.Stderr, "waiting for %s to come up...\n", name)
	const maxTries = 20
	const delay = 100 * time.Millisecond
	for i := 0; i < maxTries; i++ {
		var up bool
		func() {
			resp, err := http.Get("http://localhost:" + port + healthPath)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				return
			}
			up = true
		}()
		if up {
			fmt.Fprintf(os.Stderr, "%s is up\n", name)
			return
		}
		time.Sleep(delay)
	}
	panic(name + " did not start up")
}

func stopServer(cmd *exec.Cmd, checkExitCode bool) {
	if cmd.Process != nil {
		cmd.Process.Signal(syscall.SIGTERM)
	}
	err := cmd.Wait()
	if checkExitCode {
		Expect(err).ToNot(HaveOccurred(), cmd.Path+" exited with unexpected error")
	} else {
		fmt.Fprintf(os.Stderr, "%s exited with %v (ignored)\n", cmd.Path, err)
	}
}

func findFreePorts(num int) []int {
	var ports []int
	for i := 0; i < num; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			panic(err)
		}
		defer l.Close()
		port := l.Addr().(*net.TCPAddr).Port
		ports = append(ports, port)
	}
	return ports
}

type safeBuffer struct {
	buf bytes.Buffer
	sync.Mutex
}

func (s *safeBuffer) Read(p []byte) (n int, err error) {
	s.Lock()
	defer s.Unlock()
	return s.buf.Read(p)
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.Lock()
	defer s.Unlock()
	return s.buf.Write(p)
}
