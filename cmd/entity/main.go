package main

import (
	"log"
	"os"

	"github.com/tjbdwanghaibo/cube-codegen/internal/entity"
)

func main() {
	if err := entity.Run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}
