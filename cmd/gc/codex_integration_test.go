package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

func TestResolveTemplateInjectsCodexHomePreStart(t *testing.T) {
	cityPath := t.TempDir()
	params := &agentBuildParams{
		cityName:  "city",
		cityPath:  cityPath,
		workspace: &config.Workspace{Provider: "codex"},
		providers: map[string]config.ProviderSpec{
			"codex": {Command: "codex", PromptMode: "none"},
		},
		lookPath:        stubLookPath,
		fs:              fsys.OSFS{},
		rigs:            nil,
		beaconTime:      time.Unix(0, 0),
		beadNames:       make(map[string]string),
		stderr:          io.Discard,
		sessionProvider: "tmux",
	}
	agent := &config.Agent{
		Name:     "mayor",
		Scope:    "city",
		Provider: "codex",
		WakeMode: "fresh",
	}

	tp, err := resolveTemplate(params, agent, agent.QualifiedName(), nil)
	if err != nil {
		t.Fatalf("resolveTemplate: %v", err)
	}

	found := false
	for _, entry := range tp.Hints.PreStart {
		if strings.Contains(entry, "internal codex-home") {
			found = true
			if !strings.Contains(entry, "--agent mayor") {
				t.Fatalf("codex-home PreStart missing agent flag: %q", entry)
			}
			if !strings.Contains(entry, "--workdir") {
				t.Fatalf("codex-home PreStart missing workdir flag: %q", entry)
			}
			if !strings.Contains(entry, "--fresh") {
				t.Fatalf("codex-home PreStart missing fresh flag for fresh wake mode: %q", entry)
			}
		}
	}
	if !found {
		t.Fatalf("expected codex-home PreStart, got %v", tp.Hints.PreStart)
	}
}
