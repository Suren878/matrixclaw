package daemoncmd

import (
	"os"
	"strings"
	"testing"
)

func TestSupervisorDoesNotImportConcreteClients(t *testing.T) {
	body, err := os.ReadFile("supervisor.go")
	if err != nil {
		t.Fatalf("ReadFile(supervisor.go) error = %v", err)
	}
	if strings.Contains(string(body), "clients/telegram") {
		t.Fatal("supervisor.go must use clientRegistry instead of importing clients/telegram")
	}
}
