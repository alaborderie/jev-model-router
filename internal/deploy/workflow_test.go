package deploy

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// K3s keeps its kubeconfig root-readable. Every remote kubectl invocation
// must use non-interactive sudo, rather than widening kubeconfig permissions.
func TestRemoteKubectlUsesNonInteractiveSudo(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/image.yml")
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(data)
	commands := regexp.MustCompile(`(?:sudo(?: -n)? )?kubectl\b`).FindAllString(workflow, -1)
	if len(commands) != 2 {
		t.Fatalf("expected restart and status commands, got %d", len(commands))
	}
	for _, command := range commands {
		if command != "sudo -n kubectl" {
			t.Errorf("remote command %q cannot reliably read the root-owned K3s kubeconfig", command)
		}
	}
	if !strings.Contains(workflow, " && sudo -n kubectl") || !strings.Contains(workflow, "--timeout=120s") {
		t.Error("deployment must retain fail-fast execution and bounded rollout verification")
	}
}
