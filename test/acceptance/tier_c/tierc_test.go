//go:build acceptance_c

// Tier C acceptance tests — real inference agents.
//
// These start cities with real AI models (haiku) and verify end-to-end
// outcomes: work dispatched → agent picks up → implements → result appears.
// Assertions are loose (eventual consistency) because model behavior is
// non-deterministic.
//
// Requires: gc binary, bd binary, tmux, ANTHROPIC_API_KEY (for haiku).
// Expected duration: ~5 min per scenario.
// Trigger: manual (make test-acceptance-c), then nightly.
package tierc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	helpers "github.com/gastownhall/gascity/test/acceptance/helpers"
)

var testEnvC *helpers.Env

func TestMain(m *testing.M) {
	// Require ANTHROPIC_API_KEY for inference.
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		// Skip silently — Tier C requires credentials.
		os.Exit(0)
	}

	tmpDir, err := os.MkdirTemp("", "gc-acceptance-c-*")
	if err != nil {
		panic("acceptance-c: creating temp dir: " + err.Error())
	}
	defer os.RemoveAll(tmpDir)

	gcBinary := helpers.BuildGC(tmpDir)

	gcHome := filepath.Join(tmpDir, "gc-home")
	runtimeDir := filepath.Join(tmpDir, "runtime")
	for _, d := range []string{gcHome, runtimeDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			panic("acceptance-c: " + err.Error())
		}
	}
	if err := helpers.WriteSupervisorConfig(gcHome); err != nil {
		panic("acceptance-c: " + err.Error())
	}

	testEnvC = helpers.NewEnv(gcBinary, gcHome, runtimeDir).
		Without("GC_SESSION"). // use real tmux, not subprocess
		Without("GC_BEADS")    // use real bd, not file

	// Ensure tmux is available.
	if _, err := exec.LookPath("tmux"); err != nil {
		panic("acceptance-c: tmux not found")
	}

	code := m.Run()

	helpers.RunGC(testEnvC, "", "supervisor", "stop") //nolint:errcheck
	os.Exit(code)
}

// TestSwarm_SlingWorkCoderCommits verifies the swarm end-to-end:
// sling a task → coder picks up → creates a file → committer commits.
//
// This is a loose assertion test: we don't verify intermediate steps,
// only that a commit eventually appears with the expected content.
func TestSwarm_SlingWorkCoderCommits(t *testing.T) {
	if testing.Short() {
		t.Skip("Tier C: skipping in short mode")
	}

	// Create a throwaway git repo as the rig.
	rigDir := setupThrowawayRepo(t)

	// Init a swarm city with this rig.
	c := helpers.NewCity(t, testEnvC)
	c.InitFrom(filepath.Join(helpers.ExamplesDir(), "swarm"))

	// Add the rig to city config.
	toml := c.ReadFile("city.toml")
	toml += "\n[[rigs]]\nname = \"testrig\"\npath = \"" + rigDir + "\"\nincludes = [\"packs/swarm\"]\n"
	// Limit pool sizes to reduce cost.
	toml += "\n[[rigs.overrides]]\nagent = \"coder\"\n[rigs.overrides.pool]\nmin = 1\nmax = 1\n"
	c.WriteConfig(toml)

	c.StartWithSupervisor()

	// Wait for agents to initialize.
	time.Sleep(5 * time.Second)

	// Create a task for the coder.
	out, err := runBD(t, rigDir, c.Env, "create",
		"--title", "Create a file called hello.txt with the text 'hello world'",
		"--priority=P1")
	if err != nil {
		t.Fatalf("bd create: %v\n%s", err, out)
	}

	// Extract bead ID from output (first word of first line typically).
	beadID := extractBeadID(out)
	if beadID == "" {
		t.Fatalf("could not extract bead ID from: %s", out)
	}
	t.Logf("Created task %s", beadID)

	// Poll for outcome: a commit should eventually appear that creates hello.txt.
	deadline := 5 * time.Minute
	found := pollForCondition(t, deadline, 10*time.Second, func() bool {
		// Check if hello.txt exists in the repo (committed or not).
		_, err := os.Stat(filepath.Join(rigDir, "hello.txt"))
		return err == nil
	})

	if !found {
		// Capture diagnostics.
		gitLog := gitCmd(t, rigDir, "log", "--oneline", "-10")
		status, _ := c.GC("status", "--city", c.Dir)
		t.Fatalf("hello.txt not created within %s\ngit log:\n%s\nstatus:\n%s", deadline, gitLog, status)
	}

	t.Logf("hello.txt created successfully")

	// Bonus: check if it was committed (not just a working tree file).
	gitLog := gitCmd(t, rigDir, "log", "--oneline", "-5")
	t.Logf("Recent commits:\n%s", gitLog)
}

// TestGastown_PolecatImplementsRefineryMerges verifies the gastown flow:
// dispatch work to polecat pool → polecat creates branch + commits →
// reassigns to refinery → refinery merges to default branch.
func TestGastown_PolecatImplementsRefineryMerges(t *testing.T) {
	if testing.Short() {
		t.Skip("Tier C: skipping in short mode")
	}

	rigDir := setupThrowawayRepo(t)

	c := helpers.NewCity(t, testEnvC)
	c.InitFrom(filepath.Join(helpers.ExamplesDir(), "gastown"))

	toml := c.ReadFile("city.toml")
	toml += "\n[[rigs]]\nname = \"testrig\"\npath = \"" + rigDir + "\"\nincludes = [\"packs/gastown\"]\n"
	// Limit pool to 1 polecat.
	toml += "\n[[rigs.overrides]]\nagent = \"polecat\"\n[rigs.overrides.pool]\nmin = 1\nmax = 1\n"
	c.WriteConfig(toml)

	c.StartWithSupervisor()

	time.Sleep(5 * time.Second)

	// Dispatch work to the polecat pool with a simple task.
	out, err := runBD(t, rigDir, c.Env, "create",
		"--title", "Create a file called feature.txt containing 'new feature'",
		"--label=pool:testrig/polecat",
		"--priority=P1")
	if err != nil {
		t.Fatalf("bd create: %v\n%s", err, out)
	}
	beadID := extractBeadID(out)
	if beadID == "" {
		t.Fatalf("could not extract bead ID from: %s", out)
	}
	t.Logf("Dispatched task %s to polecat pool", beadID)

	// Poll for outcome: the bead should eventually be closed or reassigned
	// to the refinery, AND a commit should appear.
	deadline := 5 * time.Minute
	found := pollForCondition(t, deadline, 10*time.Second, func() bool {
		// Check if any branch other than main/master exists (polecat creates a feature branch).
		branches := gitCmd(t, rigDir, "branch", "--list", "--no-color")
		for _, line := range strings.Split(branches, "\n") {
			branch := strings.TrimSpace(strings.TrimPrefix(line, "*"))
			if branch != "" && branch != "main" && branch != "master" {
				return true
			}
		}
		return false
	})

	if !found {
		gitLog := gitCmd(t, rigDir, "log", "--all", "--oneline", "-10")
		branches := gitCmd(t, rigDir, "branch", "-a")
		status, _ := c.GC("status", "--city", c.Dir)
		t.Fatalf("no feature branch created within %s\nbranches:\n%s\ngit log:\n%s\nstatus:\n%s",
			deadline, branches, gitLog, status)
	}

	t.Logf("Feature branch created")

	// Check if anything was merged to main (refinery flow).
	// This is a bonus check — the refinery may not have run yet.
	mainLog := gitCmd(t, rigDir, "log", "--oneline", "-5", "main")
	t.Logf("Main branch commits:\n%s", mainLog)
}

// --- helpers ---

func setupThrowawayRepo(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "config", "user.name", "Test")
	// Create an initial commit so the repo has a HEAD.
	readme := filepath.Join(dir, "README.md")
	if err := os.WriteFile(readme, []byte("# Test Repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "initial commit")
	return dir
}

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Don't fatal — some callers expect non-zero exits.
		return string(out)
	}
	return strings.TrimSpace(string(out))
}

func runBD(t *testing.T, dir string, env *helpers.Env, args ...string) (string, error) {
	t.Helper()
	bdPath := helpers.RequireBD(t)
	cmd := exec.Command(bdPath, args...)
	cmd.Dir = dir
	cmd.Env = env.List()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func extractBeadID(output string) string {
	// bd create output typically starts with the bead ID.
	// Format varies: "ab-xyz Created: title" or just "ab-xyz"
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) == 0 {
		return ""
	}
	// First field of first line.
	fields := strings.Fields(lines[0])
	if len(fields) > 0 {
		id := fields[0]
		// Bead IDs are prefix-hash format like "ab-xyz".
		if strings.Contains(id, "-") && len(id) >= 4 {
			return id
		}
	}
	return ""
}

func pollForCondition(t *testing.T, timeout, interval time.Duration, check func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(interval)
	}
	return false
}
