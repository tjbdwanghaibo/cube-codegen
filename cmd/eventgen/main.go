package main

import (
	"log"
	"os"

	"github.com/tjbdwanghaibo/roost-codegen/internal/eventgen"
)

func main() {
	if err := eventgen.Run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}
