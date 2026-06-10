package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Suren878/matrixclaw/internal/daemoncmd"
)

func main() {
	os.Exit(daemoncmd.RunCLI(context.Background(), os.Stdout, filepath.Base(os.Args[0]), os.Args[1:], daemoncmd.Run))
}
