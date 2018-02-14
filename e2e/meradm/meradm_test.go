package Meradm

import (
	"testing"

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
				Meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")

				out := MeradmList()

				Expect(out).To(ContainElement(MatchRegexp(`.*service1.*TCP.*10.1.1.1:888.*wrr.*flag-1,flag-2.*`)))
			})

			It("succeeds without scheduler flags", func() {
				Meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr")

				out := MeradmList()

				Expect(out).To(ContainElement(MatchRegexp(`.*service1.*TCP.*10.1.1.1:888.*wrr.*`)))
			})

			It("fails if scheduler is not specified", func() {
				_, err := Meradm2("service", "add", "service1", "tcp", "10.1.1.1:888")
				Expect(err).To(HaveOccurred())
			})
		})

		It("can edit an existing service", func() {
			Meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
			Meradm("service", "edit", "service1", "-s=sh", "-b=flag-3")

			out := MeradmList()

			Expect(out).To(ContainElement(MatchRegexp(`.*service1.*TCP.*10.1.1.1:888.*sh.*flag-3.*`)))
		})

		It("can delete an existing service", func() {
			Meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
			Meradm("service", "del", "service1")

			out := MeradmList()

			Expect(out).NotTo(ContainElement(MatchRegexp(`.*service1.*`)))
		})

		It("fails if required fields are unset", func() {
			_, err := Meradm2("service", "add", "service1", "tcp")
			Expect(err).To(HaveOccurred(), "should have failed validation")
			_, err = Meradm2("service", "add", "service1")
			Expect(err).To(HaveOccurred(), "should have failed validation")
		})
	})

	Describe("servers", func() {
		BeforeEach(func() {
			Meradm("service", "add", "service1", "tcp", "10.1.1.1:888", "-s=wrr", "-b=flag-1,flag-2")
		})

		It("can add a new server", func() {
			Meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq")

			out := MeradmList()

			Expect(out).To(ContainElement(MatchRegexp(`.*172.16.1.1:555.*MASQ.*2.*`)))
		})

		It("can edit a server", func() {
			Meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq")
			Meradm("server", "edit", "service1", "172.16.1.1:555", "-w=5")
			Meradm("server", "edit", "service1", "172.16.1.1:555", "-f=route")

			out := MeradmList()

			Expect(out).To(ContainElement(MatchRegexp(`.*172.16.1.1:555.*ROUTE.*5.*`)))
			Expect(out).ToNot(ContainElement(MatchRegexp(`.*MASQ.*`)))
			Expect(out).ToNot(ContainElement(MatchRegexp(`.* 2 .*`)))
		})

		It("can delete a server", func() {
			Meradm("server", "add", "service1", "172.16.1.1:555", "-w=2", "-f=masq")
			Meradm("server", "del", "service1", "172.16.1.1:555")

			out := MeradmList()

			Expect(out).ToNot(ContainElement(MatchRegexp(`.*172.16.1.1.*`)))
		})

		It("can optionally set flags", func() {
			Meradm("server", "add", "service1", "172.16.1.1:555", "-f=masq", "-w=1")
			Meradm("server", "edit", "service1", "172.16.1.1:555", "-f=route")
			Meradm("server", "edit", "service1", "172.16.1.1:555", "-w=4")

			out := MeradmList()

			Expect(out).To(ContainElement(MatchRegexp(`.*172.16.1.1:555.*ROUTE.*4.*`)))
		})

		It("requires flags are set when adding a service", func() {
			_, err := Meradm2("server", "add", "service1", "172.16.1.1:555")
			Expect(err).To(HaveOccurred())
		})
	})
})
