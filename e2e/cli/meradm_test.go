package cli

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("meradm", func() {
	Describe("--version", func() {
		It("should print out its version", func() {
			out := meradm("--version")
			Expect(out).To(ContainSubstring("version"))
		})
	})
})
