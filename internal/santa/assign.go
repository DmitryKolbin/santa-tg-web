package santa

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// Graph is a directed adjacency matrix: an edge means the sender may give a
// gift to the receiver.
type Graph struct {
	PersonCount int64
	Edges       []bool
}

func (g *Graph) SetEdge(from, to int64)      { g.Edges[from*g.PersonCount+to] = true }
func (g *Graph) HasEdge(from, to int64) bool { return g.Edges[from*g.PersonCount+to] }

var ErrNoAssignment = errors.New("нет допустимого распределения")

// FindAssignments returns receiver indices, indexed by sender. Candidate order is
// randomized; this does not sample all valid assignments uniformly.
func FindAssignments(ctx context.Context, graph Graph, singleCycle, allowMutualPairs bool) ([]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	n := int(graph.PersonCount)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if n < 2 || n > 50 || len(graph.Edges) != n*n || (n == 2 && !allowMutualPairs) {
		return nil, ErrNoAssignment
	}
	candidates := make([][]int, n)
	for from := range n {
		for _, to := range rand.Perm(n) {
			if from != to && graph.HasEdge(int64(from), int64(to)) {
				candidates[from] = append(candidates[from], to)
			}
		}
	}
	result := make([]int, n)
	for i := range result {
		result[i] = -1
	}
	if !singleCycle && allowMutualPairs {
		owners := make([]int, n)
		for i := range owners {
			owners[i] = -1
		}
		var augment func(int, []bool) bool
		augment = func(from int, seen []bool) bool {
			if ctx.Err() != nil {
				return false
			}
			for _, to := range candidates[from] {
				if seen[to] {
					continue
				}
				seen[to] = true
				if owners[to] == -1 || augment(owners[to], seen) {
					owners[to] = from
					return true
				}
			}
			return false
		}
		for _, from := range rand.Perm(n) {
			if !augment(from, make([]bool, n)) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return nil, ErrNoAssignment
			}
		}
		for to, from := range owners {
			result[from] = to
		}
	} else if singleCycle {
		// ponytail: Hamiltonian search is exponential; bounded at two seconds for 50
		// players. Use a constraint solver if larger constrained games are required.
		visited := make([]bool, n)
		visited[0] = true
		var visit func(int, int) bool
		visit = func(from, count int) bool {
			if ctx.Err() != nil {
				return false
			}
			if count == n {
				if graph.HasEdge(int64(from), 0) {
					result[from] = 0
					return true
				}
				return false
			}
			for _, to := range candidates[from] {
				if visited[to] {
					continue
				}
				visited[to] = true
				result[from] = to
				if visit(to, count+1) {
					return true
				}
				visited[to] = false
				result[from] = -1
			}
			return false
		}
		if !visit(0, 1) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, ErrNoAssignment
		}
	} else {
		// ponytail: forbidding two-cycles needs backtracking; retain the same bounded
		// search budget, replace with a constraint solver if this becomes a bottleneck.
		used := make([]bool, n)
		order := rand.Perm(n)
		var visit func(int) bool
		visit = func(count int) bool {
			if ctx.Err() != nil {
				return false
			}
			if count == n {
				return true
			}
			from, fewest := -1, n+1
			for _, f := range order {
				if result[f] != -1 {
					continue
				}
				available := 0
				for _, to := range candidates[f] {
					if !used[to] && result[to] != f {
						available++
					}
				}
				if available == 0 {
					return false
				}
				if available < fewest {
					from, fewest = f, available
				}
			}
			for _, to := range candidates[from] {
				if used[to] || result[to] == from {
					continue
				}
				result[from] = to
				used[to] = true
				if visit(count + 1) {
					return true
				}
				result[from] = -1
				used[to] = false
			}
			return false
		}
		if !visit(0) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, ErrNoAssignment
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
