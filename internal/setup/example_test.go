package setup

import "testing"

func TestExampleSetupLoads(t *testing.T) {
	if _, err := NewFileStore("../../setup.example.json").Load(); err != nil {
		t.Fatal(err)
	}
}
