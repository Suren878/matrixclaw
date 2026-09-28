package daemoncmd

import (
	"strings"
	"testing"
)

func TestOneDaemonHoldsTheDataDirectory(t *testing.T) {
	dir := t.TempDir()
	release, err := lockDataDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := lockDataDir(dir); err == nil || !strings.Contains(err.Error(), "another matrixclawd") {
		t.Fatalf("second lock err = %v", err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	again, err := lockDataDir(dir)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	_ = again()
}
