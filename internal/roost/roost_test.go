package roost

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveModsAddsRequiredDependencies(t *testing.T) {
	got, err := resolveMods([]string{"remote_entity", "sync"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"redis", "nats", "remote_entity", "sync"} {
		if !contains(got, want) {
			t.Fatalf("resolved mods %v do not contain %s", got, want)
		}
	}
}

func TestNewProjectSyncPreservesBusinessFiles(t *testing.T) {
	target := filepath.Join(t.TempDir(), "planet")
	result, root, err := NewProject(NewOptions{
		Name:     "planet",
		Module:   "example.com/planet",
		Out:      target,
		Services: []string{"game", "gate"},
		Mods:     []string{"configdata", "sync", "remote_entity"},
		Features: []string{"config", "nest"},
	})
	if err != nil {
		t.Fatalf("new project: %v", err)
	}
	if len(result.Created) == 0 || root != target {
		t.Fatalf("unexpected result: %#v root=%s", result, root)
	}
	for _, rel := range []string{
		"roost.yaml",
		"Makefile",
		"internal/bootstrap/generated.go",
		"internal/service/game/service.go",
		"configs/generated/gen_table_config.go",
		"game/bootstrap/nest.go",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s missing: %v", rel, err)
		}
	}

	servicePath := filepath.Join(root, "internal", "service", "game", "service.go")
	raw, err := os.ReadFile(servicePath)
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("\n// business-owned\n")...)
	if err := os.WriteFile(servicePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncProject(root); err != nil {
		t.Fatalf("sync project: %v", err)
	}
	after, _ := os.ReadFile(servicePath)
	if !bytes.Contains(after, []byte("business-owned")) {
		t.Fatal("sync overwrote the business service file")
	}
}

func TestAddProtocolAllocatesIDAndCheckDetectsDuplicate(t *testing.T) {
	target := filepath.Join(t.TempDir(), "planet")
	_, root, err := NewProject(NewOptions{Name: "planet", Module: "example.com/planet", Out: target, Features: []string{"protocol"}})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := Add(root, AddOptions{Kind: "protocol", Name: "PlayerLogin", Group: "game"})
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %v", paths)
	}
	raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(paths[0])))
	if !strings.Contains(string(raw), "id=10000") {
		t.Fatalf("protocol did not receive first ID:\n%s", raw)
	}
	m, _ := LoadManifest(root)
	if err := CheckIDs(root, m); err != nil {
		t.Fatalf("id check: %v", err)
	}
	duplicate := filepath.Join(root, "protocol", "def", "duplicate.go")
	if err := os.WriteFile(duplicate, []byte("package protocoldef\n//cube:msg id=10000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckIDs(root, m); err == nil {
		t.Fatal("expected duplicate ID failure")
	}
}

func TestLoadManifestRejectsUnknownFields(t *testing.T) {
	root := t.TempDir()
	raw := []byte("schema: 1\nproject:\n  name: planet\n  module: example.com/planet\nversions: {}\nservices:\n  game: {}\nunknown: true\n")
	if err := os.WriteFile(filepath.Join(root, ManifestName), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadManifest(root); err == nil {
		t.Fatal("expected strict YAML decode error")
	}
}

func TestProductionConfigRejectsDevelopmentValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("sid: 1\nredis:\n  addr: 127.0.0.1:6379\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckConfig(path, true); err == nil {
		t.Fatal("expected production config rejection")
	}
}

func TestAddCamelCaseProtocolUsesSnakeFileAndPascalTypes(t *testing.T) {
	target := filepath.Join(t.TempDir(), "planet")
	_, root, err := NewProject(NewOptions{Name: "planet", Module: "example.com/planet", Out: target, Features: []string{"protocol"}})
	if err != nil {
		t.Fatal(err)
	}
	paths, err := Add(root, AddOptions{Kind: "protocol", Name: "PlayerLogin", Group: "game"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := paths[0], "protocol/def/player_login.go"; got != want {
		t.Fatalf("path = %s, want %s", got, want)
	}
	raw, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(paths[0])))
	if !strings.Contains(string(raw), "type PlayerLoginRequest struct") {
		t.Fatalf("unexpected protocol:\n%s", raw)
	}
}

func TestReplicationPresetCompilesAsGoSource(t *testing.T) {
	m := DefaultManifest("planet", "example.com/planet", []string{"game"}, []string{"etcd"}, []string{"replication-quic", "replication-kcp", "replication-udp"})
	m.Versions.Kit = "v1.0.5"
	plan, err := renderProject(m)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := plan["internal/transport/generated.go"]
	if !ok || !bytes.Contains(file.Body, []byte("func NewQUIC")) || !bytes.Contains(file.Body, []byte("func NewKCP")) || !bytes.Contains(file.Body, []byte("func NewUDP")) {
		t.Fatalf("replication preset missing:\n%s", file.Body)
	}
}

func TestReplicationRequiresFixedKitRelease(t *testing.T) {
	m := DefaultManifest("planet", "example.com/planet", []string{"game"}, []string{"etcd"}, []string{"replication-quic"})
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "v1.0.5") {
		t.Fatalf("expected kit release guard, got %v", err)
	}
	m.Versions.Kit = "v1.0.5"
	if err := m.Validate(); err != nil {
		t.Fatalf("fixed kit release rejected: %v", err)
	}
}

func TestSyncPreflightsConflictsBeforeWriting(t *testing.T) {
	target := filepath.Join(t.TempDir(), "planet")
	_, root, err := NewProject(NewOptions{Name: "planet", Module: "example.com/planet", Out: target, Features: []string{"config"}})
	if err != nil {
		t.Fatal(err)
	}
	makefile := filepath.Join(root, "Makefile")
	before, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := filepath.Join(root, "internal", "bootstrap", "generated.go")
	if err := os.WriteFile(bootstrap, []byte("package bootstrap\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	m.Services["gate"] = ServiceSpec{Mods: []string{"etcd"}}
	if err := saveManifest(root, m); err != nil {
		t.Fatal(err)
	}
	if _, err := SyncProject(root); err == nil {
		t.Fatal("expected ownership conflict")
	}
	after, err := os.ReadFile(makefile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Makefile changed before a later preflight conflict")
	}
}
