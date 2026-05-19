package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gastownhall/gascity/internal/shellquote"
	"github.com/spf13/cobra"
)

func codexHomePath(workdir string) string {
	return filepath.Join(workdir, ".codex")
}

func appendCodexHomePreStart(prestart []string, agentName, workDir string, fresh bool) []string {
	cmd := `"${GC_BIN:-gc}" internal codex-home --agent ` +
		shellquote.Join([]string{agentName}) +
		` --workdir ` + shellquote.Join([]string{workDir})
	if fresh {
		cmd += ` --fresh`
	}
	return append(prestart, cmd)
}

func newInternalCodexHomeCmd(stdout, stderr io.Writer) *cobra.Command {
	var agentName, workdir string
	var fresh bool
	cmd := &cobra.Command{
		Use:    "codex-home",
		Short:  "Stage isolated Codex state for one managed session",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if strings.TrimSpace(agentName) == "" {
				fmt.Fprintln(stderr, "gc internal codex-home: --agent is required") //nolint:errcheck
				return errExit
			}
			if strings.TrimSpace(workdir) == "" {
				fmt.Fprintln(stderr, "gc internal codex-home: --workdir is required") //nolint:errcheck
				return errExit
			}
			cityPath, err := resolveCity()
			if err != nil {
				fmt.Fprintf(stderr, "gc internal codex-home: %v\n", err) //nolint:errcheck
				return errExit
			}
			cfg, err := loadCityConfig(cityPath, stderr)
			if err != nil {
				fmt.Fprintf(stderr, "gc internal codex-home: %v\n", err) //nolint:errcheck
				return errExit
			}
			agent, ok := resolveAgentIdentity(cfg, agentName, currentRigContext(cfg))
			if !ok {
				fmt.Fprintf(stderr, "gc internal codex-home: unknown agent %q\n", agentName) //nolint:errcheck
				return errExit
			}
			if effectiveAgentProviderFamily(&agent, cfg.Workspace.Provider, cfg.Providers) != "codex" {
				fmt.Fprintf(stdout, "gc internal codex-home: provider %q does not require Codex staging; skipping\n", agent.Provider) //nolint:errcheck
				return nil
			}
			if err := stageCodexHomeIntoWorkdir(workdir, fresh); err != nil {
				fmt.Fprintf(stderr, "gc internal codex-home: %v\n", err) //nolint:errcheck
				return errExit
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "", "qualified agent identity (dir/name or name)")
	cmd.Flags().StringVar(&workdir, "workdir", "", "agent working directory")
	cmd.Flags().BoolVar(&fresh, "fresh", false, "clear persisted Codex interactive session state before launch")
	return cmd
}

func stageCodexHomeIntoWorkdir(workdir string, fresh bool) error {
	absWorkdir, err := filepath.Abs(workdir)
	if err != nil {
		return fmt.Errorf("resolve workdir: %w", err)
	}
	codexDir := codexHomePath(absWorkdir)
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return fmt.Errorf("create codex home: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve host home: %w", err)
	}
	if err := copyFileIfExists(filepath.Join(home, ".codex", "auth.json"), filepath.Join(codexDir, "auth.json"), 0o600); err != nil {
		return fmt.Errorf("stage auth.json: %w", err)
	}
	trustTargets := expandTrustTargets(absWorkdir)
	if gitRoot, err := gitProjectRoot(absWorkdir); err == nil && gitRoot != "" {
		trustTargets = appendUniqueTrustTarget(trustTargets, gitRoot)
		trustTargets = appendTrustRealPath(trustTargets, gitRoot)
	}
	for _, target := range trustTargets {
		if err := seedCodexProjectTrust(filepath.Join(codexDir, "config.toml"), target); err != nil {
			return fmt.Errorf("seed trust for %s: %w", target, err)
		}
	}
	if fresh {
		if err := clearCodexSessionState(codexDir); err != nil {
			return fmt.Errorf("clear fresh session state: %w", err)
		}
	}
	return nil
}

func gitProjectRoot(dir string) (string, error) {
	if root, ok := findLiteralGitRoot(dir); ok {
		return root, nil
	}
	return "", fmt.Errorf("git root not found for %s", dir)
}

func findLiteralGitRoot(dir string) (string, bool) {
	current := filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", false
		}
		current = parent
	}
}

func expandTrustTargets(path string) []string {
	targets := []string{}
	targets = appendUniqueTrustTarget(targets, path)
	return appendTrustRealPath(targets, path)
}

func appendTrustRealPath(targets []string, path string) []string {
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil || strings.TrimSpace(realPath) == "" {
		return targets
	}
	return appendUniqueTrustTarget(targets, realPath)
}

func appendUniqueTrustTarget(targets []string, path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return targets
	}
	for _, existing := range targets {
		if existing == path {
			return targets
		}
	}
	return append(targets, path)
}

func clearCodexSessionState(codexDir string) error {
	stalePaths := []string{
		"history.jsonl",
		"logs_2.sqlite",
		"logs_2.sqlite-shm",
		"logs_2.sqlite-wal",
		"sessions",
		"shell_snapshots",
		"state_5.sqlite",
		"state_5.sqlite-shm",
		"state_5.sqlite-wal",
	}
	for _, rel := range stalePaths {
		if err := os.RemoveAll(filepath.Join(codexDir, rel)); err != nil {
			return err
		}
	}
	return nil
}

func copyFileIfExists(src, dst string, perm os.FileMode) error {
	data, err := os.ReadFile(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, perm)
}

func seedCodexProjectTrust(configPath, projectDir string) error {
	projectDir = strings.TrimSpace(projectDir)
	if projectDir == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	header := fmt.Sprintf("[projects.%s]", strconv.Quote(projectDir))
	if strings.Contains(string(data), header) {
		return nil
	}
	var b strings.Builder
	b.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "\n%s\ntrust_level = %q\n", header, "trusted")
	return os.WriteFile(configPath, []byte(b.String()), 0o600)
}
