package mapgen

// Map grid: every new map is GridW × GridH cells of CellSize px (1600 × 896, the client's
// 1600 × 900 view: the match screen has no camera, so a map wider than the view would be cut).
const (
	GridW    = 100
	GridH    = 56
	CellSize = 16
)

type cell struct {
	solid bool
	mat   Material
	shape Shape
}

// Grid is a tile grid being authored. Coordinates are cells: c = column 0..GridW-1 (x), r =
// row 0..GridH-1 (y, down). Drawing outside the grid is ignored.
type Grid struct {
	cells [GridH][GridW]cell
}

func (g *Grid) in(c, r int) bool { return c >= 0 && c < GridW && r >= 0 && r < GridH }

// complement pairs fill a cell together.
func complement(a, b Shape) bool {
	switch {
	case a == SlopeR && b == CeilL, a == CeilL && b == SlopeR,
		a == SlopeL && b == CeilR, a == CeilR && b == SlopeL,
		a == HalfBottom && b == HalfTop, a == HalfTop && b == HalfBottom:
		return true
	}
	return false
}

// Put draws one brick, merged with what the cell holds: a full block stays full, two shapes
// that fill the cell together become a full block, hard wins over soft.
func (g *Grid) Put(c, r int, m Material, s Shape) {
	if !g.in(c, r) {
		return
	}
	old := &g.cells[r][c]
	if old.solid {
		if m == Soft && old.mat == Hard {
			m = Hard
		}
		if old.shape == Full || complement(old.shape, s) {
			s = Full
		}
	}
	*old = cell{solid: true, mat: m, shape: s}
}

// Replace draws one brick over whatever the cell holds.
func (g *Grid) Replace(c, r int, m Material, s Shape) {
	if g.in(c, r) {
		g.cells[r][c] = cell{solid: true, mat: m, shape: s}
	}
}

// Rect fills cells c0..c1 × r0..r1 (inclusive) with full blocks.
func (g *Grid) Rect(c0, r0, c1, r1 int, m Material) {
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			g.Put(c, r, m, Full)
		}
	}
}

// Clear empties cells c0..c1 × r0..r1.
func (g *Grid) Clear(c0, r0, c1, r1 int) {
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			if g.in(c, r) {
				g.cells[r][c] = cell{}
			}
		}
	}
}

// Paint turns the solid cells of c0..c1 × r0..r1 into material m, keeping their shapes.
func (g *Grid) Paint(c0, r0, c1, r1 int, m Material) {
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			if g.in(c, r) && g.cells[r][c].solid {
				g.cells[r][c].mat = m
			}
		}
	}
}

// Chamfer fills c0..c1 × r0..r1 with its corners cut at 45°: top cuts top corners (1 = a
// slope brick in each top corner cell, 2 = one more cell in, ...), bottom the bottom ones.
// An island is Chamfer(…, 1, 3): a flat top with rounded edges over a tapering underside.
func (g *Grid) Chamfer(c0, r0, c1, r1, top, bottom int, m Material) {
	for r := r0; r <= r1; r++ {
		for c := c0; c <= c1; c++ {
			// Distance (in cells, |dc| + |dr|) from each corner; a cell nearer than the cut is
			// air, one on the cut line a slope facing that corner.
			corners := [4]struct {
				d, cut int
				s      Shape
			}{
				{(r - r0) + (c - c0), top, SlopeR},
				{(r - r0) + (c1 - c), top, SlopeL},
				{(r1 - r) + (c - c0), bottom, CeilR},
				{(r1 - r) + (c1 - c), bottom, CeilL},
			}
			s, on, air := Full, 0, false
			for _, k := range corners {
				if k.cut <= 0 {
					continue
				}
				if k.d < k.cut-1 {
					air = true
				} else if k.d == k.cut-1 {
					s, on = k.s, on+1
				}
			}
			if !air && on <= 1 { // on two cut lines at once: nothing of the cell is inside
				g.Put(c, r, m, s)
			}
		}
	}
}

// Island is a floating island: a flat top at row r over cols c0..c1, thick rows deep (the
// last bottom-1 rows tapering), its top soft rows soft over a hard core (soft = thick: all
// soft; 0: all hard).
func (g *Grid) Island(c0, c1, r, thick, bottom, soft int) {
	g.Chamfer(c0, r, c1, r+thick-1, 1, bottom, Hard)
	if soft > 0 {
		g.Paint(c0, r, c1, r+soft-1, Soft)
	}
}

// Band is a 45° girder one cell thick from column c0 to c1, its top surface at row r in
// column c0: down to the right (dir +1: ◣ over ◥) or up to the right (dir -1: ◢ over ◤).
// A flat at row r to the left of c0 meets a dir +1 band; one at row r+1 meets a dir -1 band.
// It ends at row r+(c1-c0)·dir in column c1: a flat continues at that row + 1 (dir +1) or
// that row (dir -1).
func (g *Grid) Band(c0, c1, r, dir int, m Material) {
	for i := 0; i <= c1-c0; i++ {
		c, row := c0+i, r+i*dir
		if dir > 0 {
			g.Put(c, row, m, SlopeL)
			g.Put(c, row+1, m, CeilR)
		} else {
			g.Put(c, row, m, SlopeR)
			g.Put(c, row+1, m, CeilL)
		}
	}
}

// Hill is a mound with 45° sides: its flat top over cols c0..c1 at row top, widening one cell
// per row on each side down to row base.
func (g *Grid) Hill(c0, c1, top, base int, m Material) {
	for r := top; r <= base; r++ {
		k := r - top
		g.Put(c0-k-1, r, m, SlopeR)
		g.Put(c1+k+1, r, m, SlopeL)
		for c := c0 - k; c <= c1+k; c++ {
			g.Put(c, r, m, Full)
		}
	}
}

// Peak is a pointed hill: two slope bricks at cols c, c+1 on row top, widening down to base.
func (g *Grid) Peak(c, top, base int, m Material) {
	g.Put(c, top, m, SlopeR)
	g.Put(c+1, top, m, SlopeL)
	if base > top {
		g.Hill(c, c+1, top+1, base, m)
	}
}

// Icicle hangs 2 cells wide from row r0, length rows long, ending in a point.
func (g *Grid) Icicle(c, r0, length int, m Material) {
	g.Rect(c, r0, c+1, r0+length-2, m)
	g.Put(c, r0+length-1, m, CeilR)
	g.Put(c+1, r0+length-1, m, CeilL)
}

// Mirror copies the left half (cols 0..GridW/2-1) onto the right half, flipped, so both teams
// get the same terrain.
func (g *Grid) Mirror() {
	flip := map[Shape]Shape{SlopeR: SlopeL, SlopeL: SlopeR, CeilR: CeilL, CeilL: CeilR}
	for r := 0; r < GridH; r++ {
		for c := 0; c < GridW/2; c++ {
			x := g.cells[r][c]
			if f, ok := flip[x.shape]; ok {
				x.shape = f
			}
			g.cells[r][GridW-1-c] = x
		}
	}
}

// Tiles is the grid as config_maps.tiles: rows of brick type ids, 0 = air.
func (g *Grid) Tiles() [][]int {
	out := make([][]int, GridH)
	for r := range out {
		out[r] = make([]int, GridW)
		for c := range out[r] {
			if x := g.cells[r][c]; x.solid {
				out[r][c] = BrickID(x.mat, x.shape)
			}
		}
	}
	return out
}
