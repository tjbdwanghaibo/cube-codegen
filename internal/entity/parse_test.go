package entity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDir(t *testing.T) {
	dir, err := filepath.Abs("./testdata")
	if err != nil {
		t.Fatal(err)
	}

	entities, pkg, err := parseDir(dir)
	if err != nil {
		t.Fatalf("parseDir: %v", err)
	}

	if pkg != "testdata" {
		t.Fatalf("expected package 'testdata', got %q", pkg)
	}

	if len(entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(entities))
	}

	ent := entities[0]
	if ent.Name != "Player" {
		t.Fatalf("expected entity name 'Player', got %q", ent.Name)
	}
	if ent.EntityKind != "EntityKindPlayer" {
		t.Fatalf("expected entityKind 'EntityKindPlayer', got %q", ent.EntityKind)
	}
	if !ent.Sync || ent.SyncTopic != "SyncTopicPlayer" || ent.SyncPacker != "clientsync.PlayerPacker" {
		t.Fatalf("sync config = enabled:%v topic:%q packer:%q", ent.Sync, ent.SyncTopic, ent.SyncPacker)
	}

	if len(ent.Components) != 2 {
		t.Fatalf("expected 2 components, got %d", len(ent.Components))
	}
	if ent.Components[0].FieldName != "bag" {
		t.Fatalf("expected first component 'bag', got %q", ent.Components[0].FieldName)
	}
	if ent.Components[0].CompType != "CompTypeBag" {
		t.Fatalf("expected CompType 'CompTypeBag', got %q", ent.Components[0].CompType)
	}
	if ent.Components[1].FieldName != "battle" {
		t.Fatalf("expected second component 'battle', got %q", ent.Components[1].FieldName)
	}

	if len(ent.Daos) != 2 {
		t.Fatalf("expected 2 daos, got %d", len(ent.Daos))
	}
	if ent.Daos[0].FieldName != "dao" {
		t.Fatalf("expected dao field 'dao', got %q", ent.Daos[0].FieldName)
	}
	if ent.Daos[0].CollName != "players" {
		t.Fatalf("expected collName 'players', got %q", ent.Daos[0].CollName)
	}
	if ent.Daos[1].FieldName != "mail" {
		t.Fatalf("expected dao field 'mail', got %q", ent.Daos[1].FieldName)
	}
	if ent.Daos[1].CollName != "mails" {
		t.Fatalf("expected collName 'mails', got %q", ent.Daos[1].CollName)
	}
}

func TestGenerate(t *testing.T) {
	dir, err := filepath.Abs("./testdata")
	if err != nil {
		t.Fatal(err)
	}

	entities, pkg, err := parseDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	tmpFile := filepath.Join(t.TempDir(), "player_gen_wire.go")
	changed, err := generate(entities[0], pkg, tmpFile, true)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !changed {
		t.Fatal("expected file to be generated")
	}

	content, err := os.ReadFile(tmpFile)
	if err != nil {
		t.Fatal(err)
	}

	// Verify key content
	s := string(content)
	checks := []string{
		"func NewPlayer(param *entity.EntityCreateParam)",
		"Category: entity.MustEntityCategoryOfKind(EntityKindPlayer)",
		"param.NormalizeID(EntityKindPlayer)",
		`Topic:         "SyncTopicPlayer"`,
		"PackerFactory: clientsync.PlayerPacker",
		"e.EntityBase = entity.NewEntityBase(param.Id, param.Category, false, param.Kind)",
		"func (e *Player) Base() *entity.EntityBase",
		"func (e *Player) BagComp() *BagComponent",
		"func (e *Player) BattleComp() *BattleComponent",
		"func (e *Player) Dao() *PlayerDao",
		"func (e *Player) MailDao() *MailDao",
		"entity.CreateComponent(CompTypeBag, e, param)",
		"entity.CreateComponent(CompTypeBattle, e, param)",
		"e.dao = NewPlayerDao()",
		"e.mail = NewMailDao()",
		"e.dao.DbName()",
		"e.dao.CollName()",
		"e.dao.Tracker.TakePersistDirty()",
		"e.dao.MarshalPersist(mask)",
		"e.mail.DbName()",
		"e.mail.CollName()",
		"e.mail.Tracker.TakePersistDirty()",
		"e.mail.MarshalPersist(mask)",
		"func (e *Player) generatedOnClear()",
		"func (e *Player) generatedOnDestroy(reason entity.EntityDestroyReason)",
		"func (e *Player) ApplyRemoteSync(collection string, data []byte, version int64) error",
		"func (e *Player) OnDataChange(data []byte, version int64)",
		"func (e *Player) Snapshot() []checkpoint.SaveItem",
	}
	for _, check := range checks {
		if !contains(s, check) {
			t.Errorf("generated code missing: %q", check)
		}
	}

	// Second run should be unchanged
	changed, err = generate(entities[0], pkg, tmpFile, false)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("expected no change on second run")
	}
}

func TestToSnake(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Player", "player"},
		{"PlayerBase", "player_base"},
		{"HTTPServer", "h_t_t_p_server"},
		{"A", "a"},
	}
	for _, c := range cases {
		got := toSnake(c.in)
		if got != c.want {
			t.Errorf("toSnake(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && stringContains(s, substr)
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
