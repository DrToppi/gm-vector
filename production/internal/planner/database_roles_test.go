package planner

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseRoleInitialization(t *testing.T) {
	dir := t.TempDir()
	if err := Initialize(dir, "https://localhost:9443"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var runtime RuntimeConfig
	if err := json.Unmarshal(b, &runtime); err != nil {
		t.Fatal(err)
	}
	migration, err := LoadMigrationConfig(filepath.Join(dir, "migration-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	ru, err := url.Parse(runtime.DatabaseURL)
	if err != nil {
		t.Fatal("invalid runtime URL")
	}
	mu, err := url.Parse(migration.DatabaseURL)
	if err != nil {
		t.Fatal("invalid migration URL")
	}
	if ru.User.Username() != "planner_runtime" || mu.User.Username() != "planner_migrator" || migration.RuntimeRole != "planner_runtime" {
		t.Fatal("incorrect role separation")
	}
	rp, _ := ru.User.Password()
	mp, _ := mu.User.Password()
	bootstrap, err := os.ReadFile(filepath.Join(dir, "database.password"))
	if err != nil {
		t.Fatal(err)
	}
	if rp == "" || mp == "" || rp == mp || rp == string(bootstrap) || mp == string(bootstrap) {
		t.Fatal("credentials must be independent")
	}
	info, err := os.Stat(filepath.Join(dir, "migration-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("migration config must be private")
	}
	b, err = os.ReadFile(filepath.Join(dir, "migration-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(b, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatal("migration config must not include application secrets")
	}
}

func TestSetupFilesPrivateAndNonOverwriting(t *testing.T) {
	dir := t.TempDir()
	if err := Initialize(dir, "https://planner.example"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"instance.id", "encryption.key", "config.json", "database.password"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("insecure setup file", name)
		}
	}
	for _, name := range []string{"bootstrap.token", "setup-url.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatal("obsolete first-run credential generated", name)
		}
	}
	identity, _ := os.ReadFile(filepath.Join(dir, "instance.id"))
	if len(identity) != 32 || !validID.Match(identity) {
		t.Fatal("invalid installation identity")
	}
	before, _ := os.ReadFile(filepath.Join(dir, "encryption.key"))
	if err := Initialize(dir, "https://planner.example"); err == nil {
		t.Fatal("second initialization accepted")
	}
	after, _ := os.ReadFile(filepath.Join(dir, "encryption.key"))
	if string(before) != string(after) {
		t.Fatal("secret overwritten")
	}
	for _, origin := range []string{"http://example.org", "https://user:secret@example.org", "https://example.org/path", "https://example.org?x=y"} {
		if err := Initialize(t.TempDir(), origin); err == nil {
			t.Fatal("unsafe origin accepted")
		}
	}
}
