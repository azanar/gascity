package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/beads"
)

func TestCmdLLMRunCursorTextOutput(t *testing.T) {
	dir := t.TempDir()
	restorePath := prependPath(t, dir)
	defer restorePath()
	writeExecutable(t, filepath.Join(dir, "cursor-agent"), `#!/bin/sh
if [ "$1" = "whoami" ]; then
  echo "Logged in"
  exit 0
fi
printf 'CURSOR:%s' "$*"
`)

	var stdout, stderr bytes.Buffer
	code := cmdLLMRun(llmRunOptions{
		Backend: "cursor",
		Model:   "composer-2-fast",
		Prompt:  "hello world",
		Format:  "text",
		Timeout: 5 * time.Second,
	}, strings.NewReader(""), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("cmdLLMRun exit code = %d, stderr = %s", code, stderr.String())
	}
	got := stdout.String()
	if !strings.Contains(got, "CURSOR:--print --trust --output-format text --model composer-2-fast hello world") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestRunLLMResolvesBeadMetadataAndWritesBack(t *testing.T) {
	city := makeTempCity(t)
	restorePath := prependPath(t, city.binDir)
	defer restorePath()
	writeExecutable(t, filepath.Join(city.binDir, "cursor-agent"), `#!/bin/sh
if [ "$1" = "whoami" ]; then
  exit 0
fi
echo "OK"
`)
	restoreWd := chdir(t, city.root)
	defer restoreWd()

	store, err := openCityStoreAt(city.root)
	if err != nil {
		t.Fatalf("openCityStoreAt: %v", err)
	}
	created, err := store.Create(beads.Bead{
		Title:    "run llm",
		Metadata: map[string]string{llmBackendMetadataKey: "cursor", llmModelMetadataKey: "composer-2-fast"},
	})
	if err != nil {
		t.Fatalf("store.Create: %v", err)
	}

	resp, err := runLLM(llmRunOptions{
		BeadID:    created.ID,
		Prompt:    "say ok",
		Format:    "json",
		Timeout:   5 * time.Second,
		WriteBack: true,
	}, strings.NewReader(""))
	if err != nil {
		t.Fatalf("runLLM: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.OK = false: %+v", resp)
	}
	reloaded, err := openCityStoreAt(city.root)
	if err != nil {
		t.Fatalf("openCityStoreAt reload: %v", err)
	}
	updated, err := reloaded.Get(created.ID)
	if err != nil {
		t.Fatalf("store.Get: %v", err)
	}
	if got := updated.Metadata[executedLLMBackendMetadataKey]; got != "cursor" {
		t.Fatalf("executed backend = %q, want cursor", got)
	}
	if got := updated.Metadata[executedLLMModelMetadataKey]; got != "composer-2-fast" {
		t.Fatalf("executed model = %q, want composer-2-fast", got)
	}
	if got := updated.Metadata[executedLLMExitCodeMetadataKey]; got != "0" {
		t.Fatalf("executed exit code = %q, want 0", got)
	}
}

func TestRunLLMRequiresBackend(t *testing.T) {
	_, err := runLLM(llmRunOptions{
		Prompt:  "hello",
		Format:  "json",
		Timeout: time.Second,
	}, strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "backend is required") {
		t.Fatalf("err = %v, want backend required", err)
	}
}

type tempCity struct {
	root   string
	binDir string
}

func makeTempCity(t *testing.T) tempCity {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "city.toml"), []byte("[workspace]\nprovider = \"claude\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GC_BEADS", "file")
	t.Setenv("GC_CITY", root)
	t.Setenv("GC_CITY_PATH", "")
	t.Setenv("GC_CITY_ROOT", "")
	t.Setenv("GC_BEADS_SCOPE_ROOT", "")
	return tempCity{root: root, binDir: filepath.Join(root, "bin")}
}

func prependPath(t *testing.T, dir string) func() {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	if oldPath == "" {
		t.Setenv("PATH", dir)
	} else {
		t.Setenv("PATH", dir+string(os.PathListSeparator)+oldPath)
	}
	return func() { t.Setenv("PATH", oldPath) }
}

func writeExecutable(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(old); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}
}
