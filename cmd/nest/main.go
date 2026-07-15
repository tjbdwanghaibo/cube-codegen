package main

import (
	"log"
	"os"

	"github.com/tjbdwanghaibo/cube-codegen/internal/nest"
)

func main() {
	if err := nest.Run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}
