package cli

import (
	"os/exec"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("merlin", func() {
	Describe("--version", func() {
		It("should print out its version", func() {
			out, err := exec.Command("merlin", "--version").CombinedOutput()
			Expect(err).ToNot(HaveOccurred())
			Expect(string(out)).To(ContainSubstring("version"))
		})
	})
})
