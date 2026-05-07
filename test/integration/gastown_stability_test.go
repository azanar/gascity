//go:build integration

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const cityStabilityEnv = "GC_INTEGRATION_CITY_STABILITY"

// TestGastown_NamedCityStaysStableThroughStatus is an opt-in real-city guardrail
// for the mayor/tmux collapse we kept hitting locally. It uses real tmux,
// the real controller, and a timed soak window around `gc status`.
func TestGastown_NamedCityStaysStableThroughStatus(t *testing.T) {
	if os.Getenv(cityStabilityEnv) != "1" {
		t.Skipf("set %s=1 to run the long real-city stability guardrail", cityStabilityEnv)
	}
	if usingSubprocess() {
		t.Skip("requires tmux session provider")
	}

	agents := []gasTownAgent{
		{Name: "mayor", StartCommand: "sleep 3600"},
		{Name: "deacon", StartCommand: "sleep 3600"},
		{Name: "boot", StartCommand: "sleep 3600"},
	}
	cityDir := setupGasTownCityNoGuard(t, agents)
	expected := gasTownExpectedSessions(agents)

	initialPID := requireTmuxServerPID(t, cityDir)
	soakTmuxNamedSet(t, cityDir, expected, initialPID, 45*time.Second)

	out, err := gc(cityDir, "status")
	if err != nil {
		t.Fatalf("gc status failed: %v\noutput:\n%s\nlog tail:\n%s", err, out, supervisorLogTailForCity(t, cityDir))
	}
	if !strings.Contains(out, "mayor") {
		t.Fatalf("gc status missing mayor:\n%s", out)
	}
	if !strings.Contains(out, "Named sessions:") {
		t.Fatalf("gc status missing named sessions section:\n%s", out)
	}

	soakTmuxNamedSet(t, cityDir, expected, initialPID, 45*time.Second)
}

func soakTmuxNamedSet(t *testing.T, cityDir string, expected []string, wantPID int, duration time.Duration) {
	t.Helper()

	socketName := filepath.Base(cityDir)
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		gotPID, sessions := tmuxServerState(t, socketName)
		if gotPID != wantPID {
			t.Fatalf("tmux server pid changed for %s: got %d want %d\nsessions:\n%s\nlog tail:\n%s", cityDir, gotPID, wantPID, sessions, supervisorLogTailForCity(t, cityDir))
		}
		for _, agent := range expected {
			sessionName := strings.ReplaceAll(agent, "/", "--")
			if !strings.Contains(sessions, sessionName) {
				t.Fatalf("tmux session %q missing during soak for %s\nsessions:\n%s\nlog tail:\n%s", sessionName, cityDir, sessions, supervisorLogTailForCity(t, cityDir))
			}
		}
		time.Sleep(2 * time.Second)
	}
}

func requireTmuxServerPID(t *testing.T, cityDir string) int {
	t.Helper()
	socketName := filepath.Base(cityDir)
	pid, sessions := tmuxServerState(t, socketName)
	if pid == 0 {
		t.Fatalf("tmux server pid missing for %s\nsessions:\n%s\nlog tail:\n%s", cityDir, sessions, supervisorLogTailForCity(t, cityDir))
	}
	return pid
}

func tmuxServerState(t *testing.T, socketName string) (int, string) {
	t.Helper()

	pidCmd := exec.Command("tmux", "-L", socketName, "display-message", "-p", "#{pid}")
	pidOut, pidErr := pidCmd.CombinedOutput()
	listCmd := exec.Command("tmux", "-L", socketName, "list-sessions", "-F", "#{session_name}")
	listOut, listErr := listCmd.CombinedOutput()

	if pidErr != nil || listErr != nil {
		t.Fatalf("tmux server unavailable on socket %q\npid err: %v\npid out:\n%s\nlist err: %v\nlist out:\n%s", socketName, pidErr, pidOut, listErr, listOut)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidOut)))
	if err != nil {
		t.Fatalf("parsing tmux pid %q on socket %q: %v", strings.TrimSpace(string(pidOut)), socketName, err)
	}
	return pid, string(listOut)
}

func supervisorLogTailForCity(t *testing.T, cityDir string) string {
	t.Helper()
	env := parseEnvList(commandEnvForDir(cityDir, false))
	if env["GC_HOME"] == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(env["GC_HOME"], "supervisor.log"))
	if err != nil {
		return fmt.Sprintf("read supervisor.log: %v", err)
	}
	return tailText(string(data), 120)
}
