package entity

import (
	"flag"
	"io"
	"os"
)

func Run(args []string, stdout io.Writer) error {
	oldArgs, oldFlags := os.Args, flag.CommandLine
	defer func() { os.Args, flag.CommandLine = oldArgs, oldFlags }()
	os.Args = append([]string{"entity"}, args...)
	flag.CommandLine = flag.NewFlagSet("entity", flag.ExitOnError)
	flag.CommandLine.SetOutput(stdout)
	main()
	return nil
}
