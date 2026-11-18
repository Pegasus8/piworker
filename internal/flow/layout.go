package flow

import "math"

// Auto-layout (docs/design/mcp.md §10): where nodes an agent added without
// a position appear on the editor's canvas.

// Layout geometry, in canvas units. A node is drawn as a box of about
// LayoutNodeWidth × LayoutNodeHeight from its position (top-left).
const (
	LayoutNodeWidth  = 200
	LayoutNodeHeight = 80
	// LayoutColumnGap is the horizontal distance from a node to the next
	// column; LayoutRowGap the vertical distance between slots.
	LayoutColumnGap = 240
	LayoutRowGap    = 120
	// LayoutOrigin is where the first node of an empty canvas goes (x and y).
	LayoutOrigin = 100
	// MaxPosition bounds the coordinates of a node position an agent may
	// set (ApplyOps) or save (Lint): finite and within ±MaxPosition.
	MaxPosition = 1e6
)

// AutoLayout positions the nodes listed in newNodeIDs that have no position
// (the zero position counts as none; listed nodes with a position, unknown
// IDs and unlisted nodes are left alone). Each goes LayoutColumnGap to the
// right of its rightmost upstream node that has a position (at that node's
// height), or, without one, into a new column right of every other node (at
// the top of the canvas), then down in LayoutRowGap steps to the first slot
// whose box overlaps no other node. Listed nodes are placed upstream first,
// otherwise in definition order, so the result is deterministic.
func AutoLayout(f *Flow, newNodeIDs []string) {
	if f == nil || len(newNodeIDs) == 0 {
		return
	}
	listed := map[string]bool{}
	for _, id := range newNodeIDs {
		listed[id] = true
	}
	index := map[string]int{}
	for i := range f.Nodes {
		if _, dup := index[f.Nodes[i].ID]; !dup {
			index[f.Nodes[i].ID] = i
		}
	}
	pending := map[int]bool{}
	var order []int // definition order
	for i := range f.Nodes {
		n := &f.Nodes[i]
		if listed[n.ID] && index[n.ID] == i && n.Position == (Position{}) {
			pending[i] = true
			order = append(order, i)
		}
	}
	if len(order) == 0 {
		return
	}

	placed := map[int]bool{} // nodes with a position: fixed or laid out
	for i := range f.Nodes {
		if !pending[i] {
			placed[i] = true
		}
	}
	upstream := map[int][]int{}
	for _, c := range f.Connections {
		src, okS := index[c.SourceNode]
		dst, okD := index[c.TargetNode]
		if okS && okD && src != dst {
			upstream[dst] = append(upstream[dst], src)
		}
	}

	// The new column for nodes without a placed upstream node.
	columnX, columnY := float64(LayoutOrigin), float64(LayoutOrigin)
	first := true
	for i := range placed {
		p := f.Nodes[i].Position
		if first || p.X+LayoutColumnGap > columnX {
			columnX = p.X + LayoutColumnGap
		}
		if first || p.Y < columnY {
			columnY = p.Y
		}
		first = false
	}

	for len(pending) > 0 {
		next := -1
		for _, i := range order {
			if !pending[i] {
				continue
			}
			ready := true
			for _, u := range upstream[i] {
				if pending[u] {
					ready = false
					break
				}
			}
			if ready {
				next = i
				break
			}
		}
		if next < 0 { // a cycle among the new nodes: take them in order
			for _, i := range order {
				if pending[i] {
					next = i
					break
				}
			}
		}

		x, y := columnX, columnY
		right := -1
		for _, u := range upstream[next] {
			if placed[u] && (right < 0 || f.Nodes[u].Position.X > f.Nodes[right].Position.X ||
				(f.Nodes[u].Position.X == f.Nodes[right].Position.X && u < right)) {
				right = u
			}
		}
		if right >= 0 {
			x, y = f.Nodes[right].Position.X+LayoutColumnGap, f.Nodes[right].Position.Y
		}
		f.Nodes[next].Position = Position{X: x, Y: freeSlot(f, placed, x, y)}
		delete(pending, next)
		placed[next] = true
	}
}

// freeSlot returns the first of the slots y, y+LayoutRowGap, … whose box at
// x overlaps no placed node. A placed box blocks at most two slots, so one of
// the first 2·len(placed)+1 is free; the search stops there whatever the
// arithmetic does (far from the origin y+LayoutRowGap == y in float64), and
// then takes the next slot even if it overlaps.
func freeSlot(f *Flow, placed map[int]bool, x, y float64) float64 {
	slots := 2*len(placed) + 1
	for k := range slots {
		if candidate := y + float64(k)*LayoutRowGap; !overlapsPlaced(f, placed, x, candidate) {
			return candidate
		}
	}
	return y + float64(slots)*LayoutRowGap
}

// ValidPosition reports whether p is finite and within ±MaxPosition on both
// axes.
func ValidPosition(p Position) bool {
	for _, v := range []float64{p.X, p.Y} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < -MaxPosition || v > MaxPosition {
			return false
		}
	}
	return true
}

// overlapsPlaced reports whether a node box at (x, y) overlaps the box of a
// placed node.
func overlapsPlaced(f *Flow, placed map[int]bool, x, y float64) bool {
	for i := range placed {
		p := f.Nodes[i].Position
		if abs(p.X-x) < LayoutNodeWidth && abs(p.Y-y) < LayoutNodeHeight {
			return true
		}
	}
	return false
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
