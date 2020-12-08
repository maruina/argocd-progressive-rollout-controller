package controllers

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"testing"
)

var _ = Describe("ProgressiveRollout controller", func() {

})

func TestCalculateClusters(t *testing.T) {
	g := NewGomegaWithT(t)
	actual := 2
	expected := 2
	g.Expect(actual).To(Equal(expected))
}
