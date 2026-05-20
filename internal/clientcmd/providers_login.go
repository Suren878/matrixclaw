package clientcmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Suren878/matrixclaw/internal/providers/ai/openaicodex"
	appsetup "github.com/Suren878/matrixclaw/internal/setup"
)

func runProviderLoginCommand(stdout io.Writer, stderr io.Writer, binaryName string, _ *appsetup.Service, args []string) int {
	providerID := "openai-codex"
	if len(args) > 0 {
		providerID = strings.TrimSpace(args[0])
	}
	if providerID != "openai-codex" {
		fmt.Fprintf(stderr, "%s: providers login: unsupported provider %q\n", binaryName, providerID)
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 20 * time.Second}
	device, err := openaicodex.StartDeviceLogin(ctx, client)
	if err != nil {
		fmt.Fprintf(stderr, "%s: openai-codex login: %v\n", binaryName, err)
		return 1
	}
	fmt.Fprintln(stdout, "Signing in to OpenAI Codex...")
	fmt.Fprintln(stdout, "(Matrixclaw creates its own session and does not modify Codex CLI or VS Code)")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "To continue, follow these steps:")
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "  1. Open this URL in your browser:")
	fmt.Fprintf(stdout, "     %s\n\n", device.VerifyURL)
	fmt.Fprintln(stdout, "  2. Enter this code:")
	fmt.Fprintf(stdout, "     %s\n\n", device.UserCode)
	fmt.Fprintln(stdout, "Waiting for sign-in... (press Ctrl+C to cancel)")
	result, err := openaicodex.CompleteDeviceLogin(ctx, client, device)
	if err != nil {
		fmt.Fprintf(stderr, "%s: openai-codex login: %v\n", binaryName, err)
		return 1
	}
	fmt.Fprintln(stdout)
	fmt.Fprintln(stdout, "Login successful!")
	fmt.Fprintf(stdout, "  Auth state: %s\n", openaicodex.AuthStorePath())
	if source := strings.TrimSpace(result.Credentials.Source); source != "" {
		fmt.Fprintf(stdout, "  Source: %s\n", source)
	}
	return 0
}
