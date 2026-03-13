// Package gastown_test validates the Gas Town example configuration.
//
// This test ensures the example stays valid as the SDK evolves:
// city.toml parses and validates, all formulas parse, and all
// prompt template files referenced by agents exist on disk.
package gastown_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

// loadExpanded loads city.toml with full pack expansion.
func loadExpanded(t *testing.T) *config.City {
	t.Helper()
	dir := exampleDir()
	cfg, _, err := config.LoadWithIncludes(fsys.OSFS{}, filepath.Join(dir, "city.toml"))
	if err != nil {
		t.Fatalf("config.LoadWithIncludes: %v", err)
	}
	return cfg
}

func TestCityTomlParses(t *testing.T) {
	dir := exampleDir()
	cfg, err := config.Load(fsys.OSFS{}, filepath.Join(dir, "city.toml"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Workspace.Name != "gastown" {
		t.Errorf("Workspace.Name = %q, want %q", cfg.Workspace.Name, "gastown")
	}
	if len(cfg.Workspace.Includes) != 1 || cfg.Workspace.Includes[0] != "packs/gastown" {
		t.Errorf("Workspace.Includes = %v, want [packs/gastown]", cfg.Workspace.Includes)
	}
}

func TestCityTomlValidates(t *testing.T) {
	cfg := loadExpanded(t)
	if err := config.ValidateAgents(cfg.Agents); err != nil {
		t.Errorf("ValidateAgents: %v", err)
	}
}

func TestPromptFilesExist(t *testing.T) {
	dir := exampleDir()
	cfg := loadExpanded(t)
	for _, a := range cfg.Agents {
		if a.PromptTemplate == "" {
			continue
		}
		path := filepath.Join(dir, a.PromptTemplate)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("agent %q: prompt_template %q: %v", a.Name, a.PromptTemplate, err)
		}
	}
}

func TestAllFormulasExist(t *testing.T) {
	dir := exampleDir()
	formulaDir := filepath.Join(dir, "packs", "gastown", "formulas")

	entries, err := os.ReadDir(formulaDir)
	if err != nil {
		t.Fatalf("reading formulas dir: %v", err)
	}

	var count int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".formula.toml") {
			continue
		}
		count++
	}

	if count != 6 {
		t.Errorf("found %d formula files, want 6", count)
	}
}

func TestAllPromptTemplatesExist(t *testing.T) {
	dir := exampleDir()
	promptDir := filepath.Join(dir, "packs", "gastown", "prompts")

	entries, err := os.ReadDir(promptDir)
	if err != nil {
		t.Fatalf("reading prompts dir: %v", err)
	}

	var count int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md.tmpl") {
			continue
		}
		count++
		t.Run(e.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(promptDir, e.Name()))
			if err != nil {
				t.Fatalf("reading %s: %v", e.Name(), err)
			}
			if len(data) == 0 {
				t.Errorf("%s is empty", e.Name())
			}
		})
	}

	if count != 7 {
		t.Errorf("found %d prompt template files, want 7", count)
	}
}

func TestAgentNudgeField(t *testing.T) {
	cfg := loadExpanded(t)

	// Verify nudge is populated for agents that have it.
	nudgeCounts := 0
	for _, a := range cfg.Agents {
		if a.Nudge != "" {
			nudgeCounts++
		}
	}
	if nudgeCounts == 0 {
		t.Error("no agents have nudge configured")
	}
}

func TestFormulasDir(t *testing.T) {
	cfg := loadExpanded(t)
	// Formulas come from packs, not from city.toml directly.
	// FormulaLayers.City should have formula dirs from both packs.
	if len(cfg.FormulaLayers.City) == 0 {
		t.Fatal("FormulaLayers.City is empty, want pack formulas layers")
	}
	wantSuffixes := []string{
		filepath.Join("packs", "maintenance", "formulas"),
		filepath.Join("dolt", "formulas"),
		filepath.Join("packs", "gastown", "formulas"),
	}
	for _, suffix := range wantSuffixes {
		found := false
		for _, d := range cfg.FormulaLayers.City {
			if strings.HasSuffix(d, suffix) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("FormulaLayers.City = %v, want entry ending with %s", cfg.FormulaLayers.City, suffix)
		}
	}
}

func TestPackDirsPopulated(t *testing.T) {
	cfg := loadExpanded(t)
	if len(cfg.PackDirs) == 0 {
		t.Fatal("PackDirs is empty after expansion")
	}
	// Should have pack dirs from maintenance, dolt, and gastown packs.
	var hasMaintenance, hasDolt, hasGastown bool
	for _, d := range cfg.PackDirs {
		if strings.HasSuffix(d, filepath.Join("packs", "maintenance")) {
			hasMaintenance = true
		}
		if strings.HasSuffix(d, "dolt") && !strings.HasSuffix(d, "dolt-health") {
			hasDolt = true
		}
		if strings.HasSuffix(d, filepath.Join("packs", "gastown")) {
			hasGastown = true
		}
	}
	if !hasMaintenance {
		t.Errorf("PackDirs missing maintenance: %v", cfg.PackDirs)
	}
	if !hasDolt {
		t.Errorf("PackDirs missing dolt: %v", cfg.PackDirs)
	}
	if !hasGastown {
		t.Errorf("PackDirs missing gastown: %v", cfg.PackDirs)
	}
}

func TestGlobalFragmentsParsed(t *testing.T) {
	dir := exampleDir()
	cfg, err := config.Load(fsys.OSFS{}, filepath.Join(dir, "city.toml"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(cfg.Workspace.GlobalFragments) == 0 {
		t.Fatal("Workspace.GlobalFragments is empty")
	}
	found := false
	for _, f := range cfg.Workspace.GlobalFragments {
		if f == "command-glossary" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GlobalFragments = %v, want command-glossary", cfg.Workspace.GlobalFragments)
	}
}

func TestDaemonConfig(t *testing.T) {
	dir := exampleDir()
	cfg, err := config.Load(fsys.OSFS{}, filepath.Join(dir, "city.toml"))
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.Daemon.PatrolInterval != "30s" {
		t.Errorf("Daemon.PatrolInterval = %q, want %q", cfg.Daemon.PatrolInterval, "30s")
	}
	if cfg.Daemon.MaxRestartsOrDefault() != 5 {
		t.Errorf("Daemon.MaxRestarts = %d, want 5", cfg.Daemon.MaxRestartsOrDefault())
	}
	if cfg.Daemon.RestartWindow != "1h" {
		t.Errorf("Daemon.RestartWindow = %q, want %q", cfg.Daemon.RestartWindow, "1h")
	}
	if cfg.Daemon.ShutdownTimeout != "5s" {
		t.Errorf("Daemon.ShutdownTimeout = %q, want %q", cfg.Daemon.ShutdownTimeout, "5s")
	}
}

// packFileConfig mirrors the pack.toml structure for test parsing.
type packFileConfig struct {
	Pack   config.PackMeta `toml:"pack"`
	Agents []config.Agent  `toml:"agent"`
}

func TestCombinedPackParses(t *testing.T) {
	dir := exampleDir()
	topoPath := filepath.Join(dir, "packs", "gastown", "pack.toml")

	data, err := os.ReadFile(topoPath)
	if err != nil {
		t.Fatalf("reading pack.toml: %v", err)
	}

	var tc packFileConfig
	if _, err := toml.Decode(string(data), &tc); err != nil {
		t.Fatalf("parsing pack.toml: %v", err)
	}

	if tc.Pack.Name != "gastown" {
		t.Errorf("[pack] name = %q, want %q", tc.Pack.Name, "gastown")
	}
	if tc.Pack.Schema != 1 {
		t.Errorf("[pack] schema = %d, want 1", tc.Pack.Schema)
	}

	// Expect 7 agents: gastown's own 6 + themed dog (overrides maintenance fallback).
	want := map[string]bool{
		"mayor": false, "deacon": false, "boot": false,
		"witness": false, "refinery": false, "polecat": false,
		"dog": false,
	}
	for _, a := range tc.Agents {
		if _, ok := want[a.Name]; ok {
			want[a.Name] = true
		} else {
			t.Errorf("unexpected pack agent %q", a.Name)
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("missing pack agent %q", name)
		}
	}
	if len(tc.Agents) != 7 {
		t.Errorf("pack has %d agents, want 7", len(tc.Agents))
	}

	// Verify city_agents list (dog from gastown overrides maintenance fallback).
	cityAgents := map[string]bool{
		"mayor": false, "deacon": false, "boot": false, "dog": false,
	}
	for _, ca := range tc.Pack.CityAgents {
		if _, ok := cityAgents[ca]; ok {
			cityAgents[ca] = true
		} else {
			t.Errorf("unexpected city_agent %q", ca)
		}
	}
	for name, found := range cityAgents {
		if !found {
			t.Errorf("missing city_agent %q", name)
		}
	}
}

func TestPackPromptFilesExist(t *testing.T) {
	dir := exampleDir()
	topoDir := filepath.Join(dir, "packs", "gastown")
	topoPath := filepath.Join(topoDir, "pack.toml")

	data, err := os.ReadFile(topoPath)
	if err != nil {
		t.Fatalf("reading pack.toml: %v", err)
	}

	var tc packFileConfig
	if _, err := toml.Decode(string(data), &tc); err != nil {
		t.Fatalf("parsing pack.toml: %v", err)
	}

	for _, a := range tc.Agents {
		if a.PromptTemplate == "" {
			continue
		}
		// Paths in pack are relative to pack dir.
		path := filepath.Join(topoDir, a.PromptTemplate)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("agent %q: prompt_template %q resolves to %q: %v",
				a.Name, a.PromptTemplate, path, err)
		}
	}
}

func TestCityAgentsFilter(t *testing.T) {
	// Verify config.LoadWithIncludes with both packs produces
	// only city-scoped agents when no rigs are registered.
	// Dog from maintenance + mayor/deacon/boot from gastown = 4.
	cfg := loadExpanded(t)

	cityAgents := map[string]bool{"mayor": true, "deacon": true, "boot": true, "dog": true}
	for _, a := range cfg.Agents {
		if !cityAgents[a.Name] {
			t.Errorf("unexpected agent %q — should be filtered out without rigs", a.Name)
		}
		if a.Dir != "" {
			t.Errorf("city agent %q: dir = %q, want empty", a.Name, a.Dir)
		}
	}
	if len(cfg.Agents) != 4 {
		t.Errorf("got %d agents, want 4 city-scoped agents", len(cfg.Agents))
	}
}

func TestMaintenancePackParses(t *testing.T) {
	dir := exampleDir()
	topoPath := filepath.Join(dir, "packs", "maintenance", "pack.toml")

	data, err := os.ReadFile(topoPath)
	if err != nil {
		t.Fatalf("reading pack.toml: %v", err)
	}

	var tc packFileConfig
	if _, err := toml.Decode(string(data), &tc); err != nil {
		t.Fatalf("parsing pack.toml: %v", err)
	}

	if tc.Pack.Name != "maintenance" {
		t.Errorf("[pack] name = %q, want %q", tc.Pack.Name, "maintenance")
	}
	if tc.Pack.Schema != 1 {
		t.Errorf("[pack] schema = %d, want 1", tc.Pack.Schema)
	}

	// Maintenance has 1 agent: dog.
	if len(tc.Agents) != 1 {
		t.Errorf("pack has %d agents, want 1", len(tc.Agents))
	}
	if len(tc.Agents) > 0 && tc.Agents[0].Name != "dog" {
		t.Errorf("agent name = %q, want %q", tc.Agents[0].Name, "dog")
	}

	// city_agents should be ["dog"].
	if len(tc.Pack.CityAgents) != 1 || tc.Pack.CityAgents[0] != "dog" {
		t.Errorf("city_agents = %v, want [dog]", tc.Pack.CityAgents)
	}

	// Verify prompt file exists.
	for _, a := range tc.Agents {
		if a.PromptTemplate == "" {
			continue
		}
		topoDir := filepath.Join(dir, "packs", "maintenance")
		path := filepath.Join(topoDir, a.PromptTemplate)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("agent %q: prompt_template %q resolves to %q: %v",
				a.Name, a.PromptTemplate, path, err)
		}
	}
}

func TestMaintenanceFormulasExist(t *testing.T) {
	dir := exampleDir()
	formulaDir := filepath.Join(dir, "packs", "maintenance", "formulas")

	entries, err := os.ReadDir(formulaDir)
	if err != nil {
		t.Fatalf("reading formulas dir: %v", err)
	}

	var count int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".formula.toml") {
			continue
		}
		count++
	}

	// 3 formulas: mol-shutdown-dance + mol-dog-jsonl + mol-dog-reaper
	if count != 3 {
		t.Errorf("found %d formula files, want 3", count)
	}
}

func TestDoltHealthFormulasExist(t *testing.T) {
	dir := exampleDir()
	formulaDir := filepath.Join(dir, "..", "dolt", "formulas")

	entries, err := os.ReadDir(formulaDir)
	if err != nil {
		t.Fatalf("reading dolt formulas dir: %v", err)
	}

	var count int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".formula.toml") {
			continue
		}
		count++
	}

	// 7 formulas: 5 dog infrastructure + 2 exec (dolt-health, dolt-remotes-patrol)
	if count != 7 {
		t.Errorf("found %d formula files, want 7", count)
	}
}
