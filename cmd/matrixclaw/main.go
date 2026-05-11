package main

import (
	"os"
	"path/filepath"

	"github.com/Suren878/matrixclaw/internal/clientcmd"
)

func main() {
	os.Exit(clientcmd.Run(clientcmd.IO{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	}, filepath.Base(os.Args[0]), os.Args[1:]))
}
