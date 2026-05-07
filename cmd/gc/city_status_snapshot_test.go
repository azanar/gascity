package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/runtime"
	"github.com/gastownhall/gascity/internal/session"
)

type statusSnapshotProvider struct {
	runtime.Provider
	names          []string
	sessionNamesN  int
	isRunningN     int
}

func (p *statusSnapshotProvider) SessionNames() []string {
	p.sessionNamesN++
	return append([]string(nil), p.names...)
}

func (p *statusSnapshotProvider) IsRunning(name string) bool {
	p.isRunningN++
	return false
}

func TestCityStatusNamedSessionsUseRuntimeLiveness(t *testing.T) {
	sp := runtime.NewFake()
	dops := newFakeDrainOps()
	store := beads.NewMemStore()

	oldOpen := openCityStoreAtForStatus
	openCityStoreAtForStatus = func(string) (beads.Store, error) {
		return store, nil
	}
	t.Cleanup(func() { openCityStoreAtForStatus = oldOpen })

	cfg := &config.City{
		Workspace: config.Workspace{Name: "city"},
		Agents:    []config.Agent{{Name: "refinery"}},
		NamedSessions: []config.NamedSession{{
			Template: "refinery",
		}},
	}
	var stdout, stderr bytes.Buffer
	cityPath := filepath.Join(t.TempDir(), "city")
	sessionName := cliSessionName(cityPath, "city", "refinery", cfg.Workspace.SessionTemplate)
	if err := sp.Start(context.Background(), sessionName, runtime.Config{Command: "echo"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	snapshot := collectCityStatusSnapshot(sp, cfg, cityPath, store, &stderr)
	if len(snapshot.NamedSessions) != 1 {
		t.Fatalf("named sessions = %d, want 1", len(snapshot.NamedSessions))
	}
	if snapshot.NamedSessions[0].Status != "active" {
		t.Fatalf("named session status = %q, want active", snapshot.NamedSessions[0].Status)
	}
	code := doCityStatus(sp, dops, cfg, cityPath, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "Named sessions:") {
		t.Fatalf("stdout missing named sessions section, got:\n%s", out)
	}
	if !strings.Contains(out, "active (on_demand)") {
		t.Fatalf("stdout = %q, want active named session status", out)
	}
}

func TestCollectCityStatusSnapshotUsesProviderRunningSet(t *testing.T) {
	cityPath := "/tmp/city"
	base := runtime.NewFake()
	sp := &statusSnapshotProvider{
		Provider: base,
		names: []string{
			cliSessionName(cityPath, "city", "mayor", ""),
			cliSessionName(cityPath, "city", "refinery", ""),
		},
	}
	cfg := &config.City{
		Workspace: config.Workspace{Name: "city"},
		Agents: []config.Agent{
			{Name: "mayor", MaxActiveSessions: intPtr(1)},
			{Name: "worker", MaxActiveSessions: intPtr(1)},
			{Name: "refinery", MaxActiveSessions: intPtr(1)},
		},
		NamedSessions: []config.NamedSession{{
			Template: "refinery",
		}},
	}

	snapshot := collectCityStatusSnapshot(sp, cfg, cityPath, nil, io.Discard)
	if sp.sessionNamesN == 0 {
		t.Fatal("SessionNames was not called")
	}
	if sp.isRunningN != 0 {
		t.Fatalf("IsRunning calls = %d, want 0", sp.isRunningN)
	}
	if len(snapshot.Agents) != 3 {
		t.Fatalf("agents = %d, want 3", len(snapshot.Agents))
	}
	if !snapshot.Agents[0].Agent.Running {
		t.Fatalf("mayor running = false, want true")
	}
	if snapshot.Agents[1].Agent.Running {
		t.Fatalf("worker running = true, want false")
	}
	if !snapshot.Agents[2].Agent.Running {
		t.Fatalf("refinery running = false, want true")
	}
	if len(snapshot.NamedSessions) != 1 || snapshot.NamedSessions[0].Status != "active" {
		t.Fatalf("named session snapshot = %+v, want active refinery", snapshot.NamedSessions)
	}
}

func TestCityStatusSnapshotNilConfigUsesCityPathName(t *testing.T) {
	cityPath := filepath.Join(t.TempDir(), "city")
	snapshot := collectCityStatusSnapshot(runtime.NewFake(), nil, cityPath, nil, io.Discard)
	if snapshot.CityName != "city" {
		t.Fatalf("CityName = %q, want city", snapshot.CityName)
	}
}

func TestCityStatusJSONPreservesNilAgentsWhenEmpty(t *testing.T) {
	status := cityStatusJSONFromSnapshot(cityStatusSnapshot{CityName: "city"}, StatusSummaryJSON{})
	if status.Agents != nil {
		t.Fatalf("Agents = %#v, want nil slice", status.Agents)
	}
}

func TestCollectCitySessionCountsUsesSessionMetadataSummary(t *testing.T) {
	store := beads.NewMemStore()
	var closedID string
	for i, bead := range []beads.Bead{
		{
			Type:   session.BeadType,
			Status: "open",
			Labels: []string{session.LabelSession},
			Metadata: map[string]string{
				"state": "active",
			},
		},
		{
			Type:   session.BeadType,
			Status: "open",
			Labels: []string{session.LabelSession},
			Metadata: map[string]string{
				"state": "suspended",
			},
		},
		{
			Type:   session.BeadType,
			Status: "open",
			Labels: []string{session.LabelSession},
			Metadata: map[string]string{
				"state": "creating",
			},
		},
		{
			Type:   session.BeadType,
			Status: "open",
			Labels: []string{session.LabelSession},
			Metadata: map[string]string{
				"state": "active",
			},
		},
	} {
		created, err := store.Create(bead)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if i == 3 {
			closedID = created.ID
		}
	}
	if err := store.Close(closedID); err != nil {
		t.Fatalf("Close: %v", err)
	}

	cityPath := filepath.Join(t.TempDir(), "city")
	if err := os.MkdirAll(cityPath, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	summary, err := collectCitySessionCounts(cityPath, store, nil, nil)
	if err != nil {
		t.Fatalf("collectCitySessionCounts: %v", err)
	}
	if summary.ActiveSessions != 1 || summary.SuspendedSessions != 1 {
		t.Fatalf("summary = %+v, want 1 active and 1 suspended", summary)
	}
}

type failingStatusStore struct {
	*beads.MemStore
	failID string
	err    error
}

func (s *failingStatusStore) Get(id string) (beads.Bead, error) {
	if id == s.failID {
		return beads.Bead{}, s.err
	}
	return s.MemStore.Get(id)
}

func TestCityStatusNamedSessionLookupErrorsAreSurfaced(t *testing.T) {
	sp := runtime.NewFake()
	dops := newFakeDrainOps()
	store := &failingStatusStore{
		MemStore: beads.NewMemStore(),
		failID:   "refinery",
		err:      errors.New("store offline"),
	}

	oldOpen := openCityStoreAtForStatus
	openCityStoreAtForStatus = func(string) (beads.Store, error) {
		return store, nil
	}
	t.Cleanup(func() { openCityStoreAtForStatus = oldOpen })

	cfg := &config.City{
		Workspace: config.Workspace{Name: "city"},
		NamedSessions: []config.NamedSession{{
			Template: "refinery",
		}},
	}

	var stdout, stderr bytes.Buffer
	snapshot := collectCityStatusSnapshot(sp, cfg, "/home/user/city", store, &stderr)
	if len(snapshot.NamedSessions) != 1 {
		t.Fatalf("named sessions = %d, want 1", len(snapshot.NamedSessions))
	}
	if got := snapshot.NamedSessions[0].Status; !strings.HasPrefix(got, "lookup error:") {
		t.Fatalf("snapshot named session status = %q, want lookup error", got)
	}

	code := doCityStatus(sp, dops, cfg, "/home/user/city", &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "lookup error:") || !strings.Contains(out, "store offline") {
		t.Fatalf("stdout = %q, want surfaced store error", out)
	}
}
