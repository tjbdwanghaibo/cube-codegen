package main

import (
	"errors"
	"flag"
	"log"
	"os"

	"github.com/tjbdwanghaibo/cube-codegen/internal/dao"
)

func main() {
	if err := dao.Run(os.Args[1:], os.Stdout); err != nil && !errors.Is(err, flag.ErrHelp) {
		log.Fatal(err)
	}
}
