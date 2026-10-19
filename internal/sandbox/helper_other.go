//go:build !linux

package sandbox

import (
	"fmt"
	"os"
)

// RunHelper is the hidden HelperCommand, which only Linux has.
func RunHelper([]string) int {
	_, _ = fmt.Fprintln(os.Stderr, "matrixclaw sandbox: Landlock is Linux only")
	return 126
}
