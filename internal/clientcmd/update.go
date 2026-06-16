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
		_, _ = fmt.Fprintf(stderr, "%s: update: %v\n", binaryName, err)
		return 1
	}
	if !ok {
		_, _ = fmt.Fprintf(stdout, "%s: already up to date (%s)\n", binaryName, version.String())
		return 0
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "check" {
		_, _ = fmt.Fprintf(stdout, "%s: update available: %s -> %s\n", binaryName, update.Current, update.Latest)
		if update.URL != "" {
			_, _ = fmt.Fprintf(stdout, "%s: release: %s\n", binaryName, update.URL)
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
			_, _ = fmt.Fprint(stdout, installOut.String())
		}
		_, _ = fmt.Fprintf(stderr, "%s: update: %v\n", binaryName, err)
		return 1
	}
	if installOut.Len() > 0 {
		_, _ = fmt.Fprint(stdout, installOut.String())
	}
	_, _ = fmt.Fprintf(stdout, "%s: updated to %s\n", binaryName, update.Latest)
	_, _ = fmt.Fprintf(stdout, "%s: restart the daemon with `%s service restart` to use the new service binary\n", binaryName, binaryName)
	return 0
}

func printUpdateUsage(w io.Writer, binaryName string) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintf(w, "  %s update         Check for a new release\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s update check   Check for a new release\n", binaryName)
	_, _ = fmt.Fprintf(w, "  %s update install Install the latest release\n", binaryName)
}
