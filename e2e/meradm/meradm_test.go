package Meradm

import (
	"testing"

	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/sky-uk/merlin/e2e"
)

func TestE2EMeradm(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Meradm Suite")
}

var _ = Describe("Meradm", func() {
	BeforeEach(func() {
		StartEtcd()
		StartMerlin()
	})

	AfterEach(func() {
		StopEtcd()
		StopMerlin()
	})

	Describe("services", func() {
		Context("add service", func() {
			It("succeeds", func() {
				meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")

				out := meradmList()

				Expect(out).To(ContainElement(MatchRegexp(`.*service1.*TCP.*10.1.1.1:888.*wrr.*flag-1,flag-2.*`)))
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

		It("can add a new server", func() {
			meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq")

			out := meradmList()

			Expect(out).To(ContainElement(MatchRegexp(`.*172.16.1.1:555.*MASQ.*2.*`)))
		})

		It("can edit a server", func() {
			meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq")
			meradm("server", "edit", "service1", "172.16.1.1:555", "-w=5")
			meradm("server", "edit", "service1", "172.16.1.1:555", "-f=route")

			out := meradmList()

			Expect(out).To(ContainElement(MatchRegexp(`.*172.16.1.1:555.*ROUTE.*5.*`)))
			Expect(out).ToNot(ContainElement(MatchRegexp(`.*MASQ.*`)))
			Expect(out).ToNot(ContainElement(MatchRegexp(`.* 2 .*`)))
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
	fmt.Printf("-> %v\n", c.Args)
	err := c.Run()
	out := output.String()
	fmt.Printf("<- %s(%v)\n", out, err)
	return out, err
}
