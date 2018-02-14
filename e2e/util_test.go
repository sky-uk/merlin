package e2e

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func downloadEtcdBinary() {
	fmt.Fprintln(os.Stderr, "Downloading etcd binary from "+etcdDownloadURL)
	func() {
		out, err := os.Create(etcdTarball)
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
	func() {
		fmt.Fprintln(os.Stderr, "Verifying checksum")
		f, err := os.Open(etcdTarball)
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
			panic("invalid sha256 sum on etcd tarball " + etcdTarball)
		}
	}()
	fmt.Fprintln(os.Stderr, "Expanding etcd binary into "+buildDir)
	c := exec.Command("/bin/sh", "-c", "tar xzvf "+etcdTarball+" && cp "+etcdExpandedPath+"etcd* "+buildDir)
	c.Dir = buildDir
	if out, err := c.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, string(out))
		panic(err)
	}
}

func startEtcd(dataDir, etcdListenPort, etcdPeerPort string) *exec.Cmd {
	etcd := exec.Command(etcdBinary,
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
	return etcd
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

func stopServer(cmd *exec.Cmd) {
	if cmd.Process != nil {
		cmd.Process.Signal(syscall.SIGTERM)
	}
	if err := cmd.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, err)
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
