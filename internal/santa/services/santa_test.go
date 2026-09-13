package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DmitryKolbin/santa-tg-web/internal/santa"
)

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
func TestAssignmentsAgainstExhaustiveOracle(t *testing.T) {
	// Every directed graph without self edges through four players, all settings.
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
					result, err := FindAssignments(context.Background(), g, single, mutual)
					want := brute(g, single, mutual)
					if want && (err != nil || !valid(g, result, single, mutual)) {
						t.Fatalf("n=%d mask=%d single=%v mutual=%v: result=%v err=%v", n, mask, single, mutual, result, err)
					}
					if !want && !errors.Is(err, ErrNoAssignment) {
						t.Fatalf("unexpected solution: %v %v", result, err)
					}
				}
			}
		}
	}
}
func TestDisconnectedCyclesAndLimits(t *testing.T) {
	g := santa.Graph{PersonCount: 6, Edges: make([]bool, 36)}
	for from, to := range []int{1, 2, 0, 4, 5, 3} {
		g.SetEdge(int64(from), int64(to))
	}
	for _, mutual := range []bool{false, true} {
		result, err := FindAssignments(context.Background(), g, false, mutual)
		if err != nil || !valid(g, result, false, mutual) {
			t.Fatal(result, err)
		}
		if _, err := FindAssignments(context.Background(), g, true, mutual); !errors.Is(err, ErrNoAssignment) {
			t.Fatal(err)
		}
	}
	for _, n := range []int{0, 1, 51} {
		if _, err := FindAssignments(context.Background(), santa.Graph{PersonCount: int64(n), Edges: make([]bool, n*n)}, false, true); !errors.Is(err, ErrNoAssignment) {
			t.Fatal(n, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FindAssignments(ctx, g, false, true); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := FindAssignments(ctx, g, true, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestFiftyPlayers(t *testing.T) {
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
			result, err := FindAssignments(context.Background(), g, single, mutual)
			if err != nil || !valid(g, result, single, mutual) {
				t.Fatal(result, err)
			}
		}
	}
}
