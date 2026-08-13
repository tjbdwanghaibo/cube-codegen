// tool/eventgen generates event type constants, Type() methods, and handler dispatch code.
//
// Usage:
//
//	go run ./tool/eventgen -def ./event/def -out ./event -pkg event -game ./game
//
// Phase 1: Scans -def for structs prefixed with "Event", generates into -out:
//   - event_def_gen.go   — copied runtime event structs
//   - event_type_gen.go  — EventType constants
//   - event_type_impl_gen.go  — Type() method implementations
//
// Phase 2: Scans -game for DealEventXXX methods, generates:
//   - <receiver>_event_gen.go — InitSub() + SyncHandleEvent()
package eventgen

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tjbdwanghaibo/roost-codegen/internal/project"
)

func main() {
	defDir := flag.String("def", "./event/def", "directory containing event definitions")
	outDir := flag.String("out", "./event", "output directory for generated event code")
	pkg := flag.String("pkg", "event", "generated package name")
	gameDir := flag.String("game", "", "game directory to scan for DealEventXXX handlers")
	eventPkg := flag.String("eventpkg", "", "import path for event package (default: derive from -out)")
	force := flag.Bool("force", false, "force regeneration")
	flag.Parse()

	absDefDir, err := filepath.Abs(*defDir)
	if err != nil {
		log.Fatalf("resolve def dir: %v", err)
	}
	absOutDir, err := filepath.Abs(*outDir)
	if err != nil {
		log.Fatalf("resolve out dir: %v", err)
	}
	if err := os.MkdirAll(absOutDir, 0755); err != nil {
		log.Fatalf("create out dir: %v", err)
	}
	if *eventPkg == "" {
		info, err := project.Discover(absOutDir)
		if err != nil {
			log.Fatalf("discover event module: %v", err)
		}
		*eventPkg, err = info.ImportPath(absOutDir)
		if err != nil {
			log.Fatalf("derive event import: %v", err)
		}
	}

	// Phase 1: generate event types
	events, err := parseEventDir(absDefDir)
	if err != nil {
		log.Fatalf("parse: %v", err)
	}

	if len(events) == 0 {
		fmt.Printf("no event structs found in %s\n", absDefDir)
		return
	}

	defFile := filepath.Join(absOutDir, "event_def_gen.go")
	changed, err := generateDefs(events, *pkg, defFile, *force)
	if err != nil {
		log.Fatalf("generate defs: %v", err)
	}
	if changed {
		fmt.Printf("generated: %s\n", defFile)
	} else {
		fmt.Printf("unchanged: %s\n", defFile)
	}

	typeFile := filepath.Join(absOutDir, "event_type_gen.go")
	changed, err = generateTypes(events, *pkg, typeFile, *force)
	if err != nil {
		log.Fatalf("generate types: %v", err)
	}
	if changed {
		fmt.Printf("generated: %s\n", typeFile)
	} else {
		fmt.Printf("unchanged: %s\n", typeFile)
	}

	typeImplFile := filepath.Join(absOutDir, "event_type_impl_gen.go")
	changed, err = generateTypeImpl(events, *pkg, typeImplFile, *force)
	if err != nil {
		log.Fatalf("generate type impl: %v", err)
	}
	if changed {
		fmt.Printf("generated: %s\n", typeImplFile)
	} else {
		fmt.Printf("unchanged: %s\n", typeImplFile)
	}

	// Phase 2: generate handler dispatch code
	if *gameDir != "" {
		absGameDir, err := filepath.Abs(*gameDir)
		if err != nil {
			log.Fatalf("resolve game dir: %v", err)
		}
		if err := scanGameDir(absGameDir, *eventPkg, *force); err != nil {
			log.Fatalf("scan game dir: %v", err)
		}
	}
}
