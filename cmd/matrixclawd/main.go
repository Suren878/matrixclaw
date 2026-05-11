package main

import (
	"context"
	"log"

	"github.com/Suren878/matrixclaw/internal/daemoncmd"
)

func main() {
	if err := daemoncmd.Run(context.Background()); err != nil {
		log.Fatalf("daemon: %v", err)
	}
}
