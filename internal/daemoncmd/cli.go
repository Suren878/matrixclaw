package daemoncmd

import (
	"context"
	"fmt"
	"io"
	"strings"
)

func RunCLI(ctx context.Context, stdout io.Writer, binaryName string, args []string, run func(context.Context) error) int {
	if ctx == nil {
		ctx = context.Background()
	}
	binaryName = strings.TrimSpace(binaryName)
	if binaryName == "" {
		binaryName = "matrixclawd"
	}
	if len(args) > 0 {
		switch strings.TrimSpace(args[0]) {
		case "-h", "--help", "help":
			printDaemonUsage(stdout, binaryName)
			return 0
		default:
			fmt.Fprintf(stdout, "%s: unknown argument %q\n", binaryName, args[0])
			printDaemonUsage(stdout, binaryName)
			return 2
		}
	}
	if run == nil {
		run = Run
	}
	if err := run(ctx); err != nil {
		fmt.Fprintf(stdout, "%s: daemon: %v\n", binaryName, err)
		return 1
	}
	return 0
}

func printDaemonUsage(w io.Writer, binaryName string) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, "Usage:\n")
	fmt.Fprintf(w, "  %s          Run the matrixclaw daemon\n", binaryName)
	fmt.Fprintf(w, "  %s --help   Print this help\n", binaryName)
}
