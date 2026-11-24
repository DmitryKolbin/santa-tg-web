package santa_test

import (
	"context"
	"testing"
	"time"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSanta(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Santa Suite")
}

func valid(g santa.Graph, result []int, single, mutual bool) bool {
	n := int(g.PersonCount)
	if len(result) != n || n < 2 {
		return false
	}
	used := make([]bool, n)
	for from, to := range result {
		if to < 0 || to >= n || to == from || used[to] || !g.HasEdge(int64(from), int64(to)) {
			return false
		}
		used[to] = true
		if !mutual && result[to] == from {
			return false
		}
	}
	if single {
		seen := make([]bool, n)
		at := 0
		for range n {
			if seen[at] {
				return false
			}
			seen[at] = true
			at = result[at]
		}
		return at == 0
	}
	return true
}

func brute(g santa.Graph, single, mutual bool) bool {
	n := int(g.PersonCount)
	result := make([]int, n)
	used := make([]bool, n)
	var visit func(int) bool
	visit = func(from int) bool {
		if from == n {
			return valid(g, result, single, mutual)
		}
		for to := range n {
			if !used[to] {
				result[from] = to
				used[to] = true
				if visit(from + 1) {
					return true
				}
				used[to] = false
			}
		}
		return false
	}
	return visit(0)
}

var _ = Describe("Assignment service", func() {
	It("matches an exhaustive oracle for every graph through four players", func() {
		for n := 2; n <= 4; n++ {
			for mask := 0; mask < 1<<(n*(n-1)); mask++ {
				g := santa.Graph{PersonCount: int64(n), Edges: make([]bool, n*n)}
				bit := 0
				for from := range n {
					for to := range n {
						if from == to {
							continue
						}
						if mask&(1<<bit) != 0 {
							g.SetEdge(int64(from), int64(to))
						}
						bit++
					}
				}
				for _, single := range []bool{false, true} {
					for _, mutual := range []bool{false, true} {
						result, err := santa.FindAssignments(context.Background(), g, single, mutual)
						context := []any{"n", n, "mask", mask, "single", single, "mutual", mutual}
						if brute(g, single, mutual) {
							Expect(err).NotTo(HaveOccurred(), context...)
							Expect(valid(g, result, single, mutual)).To(BeTrue(), context...)
						} else {
							Expect(err).To(MatchError(santa.ErrNoAssignment), context...)
						}
					}
				}
			}
		}
	})

	It("handles disconnected cycles and invalid sizes", func() {
		g := santa.Graph{PersonCount: 6, Edges: make([]bool, 36)}
		for from, to := range []int{1, 2, 0, 4, 5, 3} {
			g.SetEdge(int64(from), int64(to))
		}
		for _, mutual := range []bool{false, true} {
			result, err := santa.FindAssignments(context.Background(), g, false, mutual)
			Expect(err).NotTo(HaveOccurred())
			Expect(valid(g, result, false, mutual)).To(BeTrue())
			_, err = santa.FindAssignments(context.Background(), g, true, mutual)
			Expect(err).To(MatchError(santa.ErrNoAssignment))
		}
		for _, n := range []int{0, 1, 51} {
			_, err := santa.FindAssignments(context.Background(), santa.Graph{PersonCount: int64(n), Edges: make([]bool, n*n)}, false, true)
			Expect(err).To(MatchError(santa.ErrNoAssignment), "n=%d", n)
		}
	})

	It("propagates context cancellation and deadline", func() {
		g := santa.Graph{PersonCount: 6, Edges: make([]bool, 36)}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := santa.FindAssignments(ctx, g, false, true)
		Expect(err).To(MatchError(context.Canceled))

		ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		DeferCleanup(cancel)
		_, err = santa.FindAssignments(ctx, g, true, false)
		Expect(err).To(MatchError(context.DeadlineExceeded))
	})

	It("supports fifty players for every setting combination", func() {
		g := santa.Graph{PersonCount: 50, Edges: make([]bool, 2500)}
		for from := range 50 {
			for to := range 50 {
				if from != to {
					g.SetEdge(int64(from), int64(to))
				}
			}
		}
		for _, single := range []bool{false, true} {
			for _, mutual := range []bool{false, true} {
				result, err := santa.FindAssignments(context.Background(), g, single, mutual)
				Expect(err).NotTo(HaveOccurred())
				Expect(valid(g, result, single, mutual)).To(BeTrue())
			}
		}
	})
})
