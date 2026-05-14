package clientcmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/Suren878/matrixclaw/internal/updater"
	"github.com/Suren878/matrixclaw/internal/version"
)

var (
	checkLatestUpdate = func(ctx context.Context, current string) (updater.Update, bool, error) {
		return updater.Checker{}.Check(ctx, current)
	}
	installUpdate = func(ctx context.Context, tag string, stdout io.Writer, stderr io.Writer) error {
		return updater.Installer{Stdout: stdout, Stderr: stderr}.Install(ctx, tag)
	}
)

func runUpdateCommand(stdout io.Writer, stderr io.Writer, binaryName string, args []string) int {
	if len(args) > 0 && isHelpArg(args[0]) {
		printUpdateUsage(stdout, binaryName)
		return 0
	}
	update, ok, err := checkLatestUpdate(context.Background(), version.String())
	if err != nil {
		fmt.Fprintf(stderr, "%s: update: %v\n", binaryName, err)
		return 1
	}
	if !ok {
		fmt.Fprintf(stdout, "%s: already up to date (%s)\n", binaryName, version.String())
		return 0
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "check" {
		fmt.Fprintf(stdout, "%s: update available: %s -> %s\n", binaryName, update.Current, update.Latest)
		if update.URL != "" {
			fmt.Fprintf(stdout, "%s: release: %s\n", binaryName, update.URL)
		}
		return 0
	}
	if strings.TrimSpace(args[0]) != "install" {
		printUpdateUsage(stdout, binaryName)
		return 2
	}
	var installOut bytes.Buffer
	if err := installUpdate(context.Background(), update.Latest, &installOut, stderr); err != nil {
		if installOut.Len() > 0 {
			fmt.Fprint(stdout, installOut.String())
		}
		fmt.Fprintf(stderr, "%s: update: %v\n", binaryName, err)
		return 1
	}
	if installOut.Len() > 0 {
		fmt.Fprint(stdout, installOut.String())
	}
	fmt.Fprintf(stdout, "%s: updated to %s\n", binaryName, update.Latest)
	fmt.Fprintf(stdout, "%s: restart the daemon with `%s service restart` to use the new service binary\n", binaryName, binaryName)
	return 0
}

func printUpdateUsage(w io.Writer, binaryName string) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s update         Check for a new release\n", binaryName)
	fmt.Fprintf(w, "  %s update check   Check for a new release\n", binaryName)
	fmt.Fprintf(w, "  %s update install Install the latest release\n", binaryName)
}
