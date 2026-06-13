package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Suren878/matrixclaw/internal/telephony/gateway"
	"github.com/Suren878/matrixclaw/internal/version"
)

func main() {
	os.Exit(runCLI(context.Background(), os.Stdout, filepath.Base(os.Args[0]), os.Args[1:]))
}

func runCLI(ctx context.Context, stdout io.Writer, binaryName string, args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "-h", "--help", "help":
			printUsage(stdout, binaryName)
			return 0
		case "version", "--version":
			_, _ = fmt.Fprintf(stdout, "%s: %s\n", binaryName, version.String())
			return 0
		default:
			_, _ = fmt.Fprintf(stdout, "%s: unknown argument %q\n", binaryName, args[0])
			printUsage(stdout, binaryName)
			return 2
		}
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := gateway.ConfigFromEnv()
	if err := gateway.Run(ctx, cfg); err != nil {
		log.Print(err)
		return 1
	}
	return 0
}

func printUsage(w io.Writer, binaryName string) {
	_, _ = fmt.Fprintf(w, "Usage:\n")
	_, _ = fmt.Fprintf(w, "  %s          Run the Matrixclaw telephony gateway\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s version  Print gateway version\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s --help   Print this help\n", binaryName)
}
