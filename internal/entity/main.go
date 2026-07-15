// tool/entity generates entity factory, snapshot, and wiring code.
//
// Usage:
//
//	go run ./tool/entity -dir ./game/player
//
// Or via go:generate in the entity definition file:
//
//	//go:generate go run github.com/tjbdwanghaibo/cube/tool/entity
//
// The generator scans for struct types annotated with the marker comment:
//
//	//cube:entity entityKind=EntityKindPlayer
//	type Player struct { ... }
//
// And produces a <entity>_gen_wire.go file containing:
//   - NewXxx(param) factory function
//   - Base/Dirty and component/DAO accessor methods
//   - Snapshot() method for checkpoint
//   - Hooks wiring (onClear, onDestroy)
package entity

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	dir := flag.String("dir", "", "directory to scan (default: GOFILE dir or cwd)")
	output := flag.String("output", "", "output file (default: <entity>_gen_wire.go)")
	force := flag.Bool("force", false, "force regeneration even if unchanged")
	flag.Parse()

	// Determine scan directory
	scanDir := *dir
	if scanDir == "" {
		// Support go:generate mode
		if gofile := os.Getenv("GOFILE"); gofile != "" {
			scanDir = filepath.Dir(gofile)
		} else {
			scanDir = "."
		}
	}

	scanDir, err := filepath.Abs(scanDir)
	if err != nil {
		log.Fatalf("failed to resolve dir: %v", err)
	}

	scanDirs := []string{scanDir}
	if *output == "" {
		scanDirs, err = findEntityDirs(scanDir)
		if err != nil {
			log.Fatalf("scan error: %v", err)
		}
	}
	if len(scanDirs) == 0 {
		fmt.Printf("no entity markers found in %s\n", scanDir)
		return
	}

	for _, dir := range scanDirs {
		// Parse all .go files in the directory
		entities, pkg, err := parseDir(dir)
		if err != nil {
			log.Fatalf("parse error: %v", err)
		}
		if len(entities) == 0 {
			continue
		}

		// Generate for each entity
		for _, ent := range entities {
			outFile := *output
			if outFile == "" {
				outFile = filepath.Join(dir, fmt.Sprintf("%s_gen_wire.go", toSnake(ent.Name)))
			}

			changed, err := generate(ent, pkg, outFile, *force)
			if err != nil {
				log.Fatalf("generate %s: %v", ent.Name, err)
			}
			if changed {
				fmt.Printf("generated: %s\n", outFile)
			} else {
				fmt.Printf("unchanged: %s\n", outFile)
			}
		}
	}
}

func findEntityDirs(root string) ([]string, error) {
	dirs := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root {
				switch entry.Name() {
				case "testdata", "vendor":
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") ||
			isGeneratedWireFile(entry.Name()) ||
			strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), "//cube:entity") {
			dirs[filepath.Dir(path)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	out := make([]string, 0, len(dirs))
	for dir := range dirs {
		out = append(out, dir)
	}
	sort.Strings(out)
	return out, nil
}
