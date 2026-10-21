// Command mapgen writes the generated battle maps (internal/game/mapgen) as migrations 036 / 037
// and configs/maps.yaml + configs/bricks.yaml. Run it from the repo root:
//
//	go run ./cmd/mapgen                    # rewrite the generated files
//	go run ./cmd/mapgen -preview DIR       # also render DIR/<mapId>.png (solid mask, spawns)
//	go run ./cmd/mapgen -check             # exit 1 when a generated file is out of date
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sort"

	"battle-squad/internal/game/match"
	"battle-squad/internal/game/mapgen"
)

func main() {
	root := flag.String("root", ".", "repo root")
	preview := flag.String("preview", "", "directory for PNG previews of the layouts")
	check := flag.Bool("check", false, "only check the generated files are up to date")
	flag.Parse()

	files := mapgen.Files()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	stale := false
	for _, p := range paths {
		full := filepath.Join(*root, p)
		if *check {
			if old, err := os.ReadFile(full); err != nil || string(old) != files[p] {
				fmt.Println("out of date:", p)
				stale = true
			}
			continue
		}
		if err := os.WriteFile(full, []byte(files[p]), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("wrote", p)
	}
	if stale {
		os.Exit(1)
	}
	if *preview != "" {
		if err := os.MkdirAll(*preview, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, m := range mapgen.Maps() {
			path := filepath.Join(*preview, m.MapID+".png")
			if err := writePreview(path, m.MapID); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Println("wrote", path)
		}
	}
}

// writePreview renders the map's terrain mask as the server builds it: soft brown, hard grey,
// the cell grid faint, team 1's spawns blue and team 2's red where the match lands them.
func writePreview(path, mapID string) error {
	var cfg = mapgen.Maps()[0]
	for _, m := range mapgen.Maps() {
		if m.MapID == mapID {
			cfg = m
		}
	}
	t := match.NewTerrainWithBricks(cfg, mapgen.BrickTypeMap())
	img := image.NewRGBA(image.Rect(0, 0, t.Width, t.Height))
	sky := color.RGBA{214, 230, 242, 255}
	gridC := color.RGBA{200, 216, 230, 255}
	soft := color.RGBA{176, 120, 72, 255}
	hard := color.RGBA{96, 100, 110, 255}
	for y := 0; y < t.Height; y++ {
		for x := 0; x < t.Width; x++ {
			c := sky
			if x%mapgen.CellSize == 0 || y%mapgen.CellSize == 0 {
				c = gridC
			}
			if t.IsSolid(float64(x), float64(y)) {
				if t.IsDestructible(x, y) {
					c = soft
				} else {
					c = hard
				}
			}
			img.Set(x, y, c)
		}
	}
	team1, team2 := cfg.TeamSpawns()
	mark := func(px, py float64, c color.RGBA) {
		sy := t.SpawnY(px, py)
		for dy := -40; dy <= 0; dy++ {
			for dx := -10; dx <= 10; dx++ {
				img.Set(int(px)+dx, int(sy)+dy, c)
			}
		}
	}
	for _, sp := range team1 {
		mark(sp.X, sp.Y, color.RGBA{40, 90, 220, 255})
	}
	for _, sp := range team2 {
		mark(sp.X, sp.Y, color.RGBA{220, 50, 50, 255})
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
