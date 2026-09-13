package santa

type Graph struct {
	PersonCount int64
	Edges       []bool
}

func (g *Graph) SetEdge(from, to int64)      { g.Edges[from*g.PersonCount+to] = true }
func (g *Graph) HasEdge(from, to int64) bool { return g.Edges[from*g.PersonCount+to] }
