package nest

import (
	"flag"
	"io"
	"os"
)

func Run(args []string, stdout io.Writer) error {
	oldArgs, oldFlags := os.Args, flag.CommandLine
	defer func() { os.Args, flag.CommandLine = oldArgs, oldFlags }()
	os.Args = append([]string{"nest"}, args...)
	flag.CommandLine = flag.NewFlagSet("nest", flag.ExitOnError)
	flag.CommandLine.SetOutput(stdout)
	main()
	return nil
}
