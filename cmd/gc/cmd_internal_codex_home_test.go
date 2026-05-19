package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInternalCodexHomeStagesAuthAndTrust(t *testing.T) {
	clearGCEnv(t)
	cityDir := t.TempDir()
	homeDir := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_HOME", t.TempDir())
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.gc): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".codex"), 0o755); err != nil {
		t.Fatalf("MkdirAll(home .codex): %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".codex", "auth.json"), []byte(`{"token":"abc"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(auth.json): %v", err)
	}

	toml := `[workspace]
name = "test-city"

[beads]
provider = "file"

[[agent]]
name = "mayor"
provider = "codex"
start_command = "codex"

[[named_session]]
template = "mayor"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(toml), 0o644); err != nil {
		t.Fatalf("WriteFile(city.toml): %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"internal", "codex-home",
		"--agent", "mayor",
		"--workdir", workdir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	authPath := filepath.Join(workdir, ".codex", "auth.json")
	if _, err := os.Stat(authPath); err != nil {
		t.Fatalf("stat(%s): %v", authPath, err)
	}
	data, err := os.ReadFile(filepath.Join(workdir, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("ReadFile(config.toml): %v", err)
	}
	header := `[projects.` + `"` + workdir + `"` + `]`
	if !strings.Contains(string(data), header) {
		t.Fatalf("config.toml missing trust header %q: %s", header, string(data))
	}
	if !strings.Contains(string(data), `trust_level = "trusted"`) {
		t.Fatalf("config.toml missing trust_level: %s", string(data))
	}
}

func TestInternalCodexHomeAlsoTrustsGitRoot(t *testing.T) {
	clearGCEnv(t)
	cityDir := t.TempDir()
	homeDir := t.TempDir()
	repoRoot := t.TempDir()
	workdir := filepath.Join(repoRoot, ".gc", "agents", "mayor")
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_HOME", t.TempDir())
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.gc): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".codex"), 0o755); err != nil {
		t.Fatalf("MkdirAll(home .codex): %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".codex", "auth.json"), []byte(`{"token":"abc"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(auth.json): %v", err)
	}
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workdir): %v", err)
	}
	runExternal(t, repoRoot, "git", "init", "-q")

	toml := `[workspace]
name = "test-city"

[beads]
provider = "file"

[[agent]]
name = "mayor"
provider = "codex"
start_command = "codex"

[[named_session]]
template = "mayor"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(toml), 0o644); err != nil {
		t.Fatalf("WriteFile(city.toml): %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"internal", "codex-home",
		"--agent", "mayor",
		"--workdir", workdir,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	data, err := os.ReadFile(filepath.Join(workdir, ".codex", "config.toml"))
	if err != nil {
		t.Fatalf("ReadFile(config.toml): %v", err)
	}
	workdirHeader := `[projects.` + `"` + workdir + `"` + `]`
	if !strings.Contains(string(data), workdirHeader) {
		t.Fatalf("config.toml missing workdir trust header %q: %s", workdirHeader, string(data))
	}
	repoRootHeader := `[projects.` + `"` + repoRoot + `"` + `]`
	if !strings.Contains(string(data), repoRootHeader) {
		t.Fatalf("config.toml missing repo-root trust header %q: %s", repoRootHeader, string(data))
	}
}

func TestInternalCodexHomeFreshClearsPersistentSessionState(t *testing.T) {
	clearGCEnv(t)
	cityDir := t.TempDir()
	homeDir := t.TempDir()
	workdir := t.TempDir()
	t.Setenv("GC_CITY", cityDir)
	t.Setenv("GC_HOME", t.TempDir())
	t.Setenv("HOME", homeDir)

	if err := os.MkdirAll(filepath.Join(cityDir, ".gc"), 0o755); err != nil {
		t.Fatalf("MkdirAll(.gc): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(homeDir, ".codex"), 0o755); err != nil {
		t.Fatalf("MkdirAll(home .codex): %v", err)
	}
	if err := os.WriteFile(filepath.Join(homeDir, ".codex", "auth.json"), []byte(`{"token":"abc"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(auth.json): %v", err)
	}

	toml := `[workspace]
name = "test-city"

[beads]
provider = "file"

[[agent]]
name = "mayor"
provider = "codex"
wake_mode = "fresh"
start_command = "codex"

[[named_session]]
template = "mayor"
`
	if err := os.WriteFile(filepath.Join(cityDir, "city.toml"), []byte(toml), 0o644); err != nil {
		t.Fatalf("WriteFile(city.toml): %v", err)
	}

	codexDir := filepath.Join(workdir, ".codex")
	if err := os.MkdirAll(filepath.Join(codexDir, "sessions", "2026"), 0o755); err != nil {
		t.Fatalf("MkdirAll(sessions): %v", err)
	}
	if err := os.MkdirAll(filepath.Join(codexDir, "shell_snapshots"), 0o755); err != nil {
		t.Fatalf("MkdirAll(shell_snapshots): %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "history.jsonl"), []byte("stale\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(history): %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "state_5.sqlite"), []byte("sqlite"), 0o600); err != nil {
		t.Fatalf("WriteFile(state): %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "state_5.sqlite-wal"), []byte("wal"), 0o600); err != nil {
		t.Fatalf("WriteFile(state wal): %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "logs_2.sqlite"), []byte("logs"), 0o600); err != nil {
		t.Fatalf("WriteFile(logs): %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "sessions", "2026", "stale.jsonl"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile(session): %v", err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "shell_snapshots", "stale.sh"), []byte("echo stale"), 0o600); err != nil {
		t.Fatalf("WriteFile(snapshot): %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"internal", "codex-home",
		"--agent", "mayor",
		"--workdir", workdir,
		"--fresh",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	if _, err := os.Stat(filepath.Join(codexDir, "auth.json")); err != nil {
		t.Fatalf("auth.json missing after fresh stage: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(codexDir, "config.toml"))
	if err != nil {
		t.Fatalf("ReadFile(config.toml): %v", err)
	}
	header := `[projects.` + `"` + workdir + `"` + `]`
	if !strings.Contains(string(data), header) {
		t.Fatalf("config.toml missing trust header %q: %s", header, string(data))
	}

	for _, stale := range []string{
		filepath.Join(codexDir, "history.jsonl"),
		filepath.Join(codexDir, "state_5.sqlite"),
		filepath.Join(codexDir, "state_5.sqlite-wal"),
		filepath.Join(codexDir, "logs_2.sqlite"),
		filepath.Join(codexDir, "sessions"),
		filepath.Join(codexDir, "shell_snapshots"),
	} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Fatalf("stale Codex state should be removed on fresh stage: %s err=%v", stale, err)
		}
	}
}
