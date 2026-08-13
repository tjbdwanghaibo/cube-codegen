package main

import (
	"log"
	"os"

	"github.com/tjbdwanghaibo/roost-codegen/internal/nest"
)

func main() {
	if err := nest.Run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}
