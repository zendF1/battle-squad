package match

import (
	"battle-squad/internal/game/gamedata"
	"sort"
)

// polygonFromBorder builds a closed polygon from a BrickBorder's 4 edges.
// Points are in brick-local coords (origin bottom-left, y-up).
// Returns points converted to pixel coords relative to cell top-left (y-down).
func polygonFromBorder(border gamedata.BrickBorder, cellSize int) []gamedata.BorderPoint {
	var poly []gamedata.BorderPoint
	edges := [][]gamedata.BorderPoint{border.Bottom, border.Right, border.Top, border.Left}
	for _, edge := range edges {
		for _, p := range edge {
			poly = append(poly, gamedata.BorderPoint{
				X: p.X,
				Y: float64(cellSize) - p.Y,
			})
		}
	}
	return poly
}

// scanlineFillPolygon fills a polygon into a boolean mask at the given cell offset.
func scanlineFillPolygon(
	mask []uint64,
	destructibleMask []uint64,
	width int,
	poly []gamedata.BorderPoint,
	offsetX, offsetY int,
	cellSize int,
	destructible bool,
) {
	if len(poly) < 3 {
		return
	}

	for y := 0; y < cellSize; y++ {
		scanY := float64(y) + 0.5

		var intersections []float64
		n := len(poly)
		for i := 0; i < n; i++ {
			j := (i + 1) % n
			y1, y2 := poly[i].Y, poly[j].Y
			if y1 == y2 {
				continue
			}
			if scanY < min64(y1, y2) || scanY >= max64(y1, y2) {
				continue
			}
			t := (scanY - y1) / (y2 - y1)
			xIntersect := poly[i].X + t*(poly[j].X-poly[i].X)
			intersections = append(intersections, xIntersect)
		}

		sort.Float64s(intersections)

		for k := 0; k+1 < len(intersections); k += 2 {
			xStart := int(intersections[k])
			xEnd := int(intersections[k+1])
			if xStart < 0 {
				xStart = 0
			}
			if xEnd > cellSize {
				xEnd = cellSize
			}
			for x := xStart; x < xEnd; x++ {
				px := offsetX + x
				py := offsetY + y
				if px >= 0 && px < width && py >= 0 {
					setBit(mask, px, py, width)
					if destructible {
						setBit(destructibleMask, px, py, width)
					}
				}
			}
		}
	}
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
