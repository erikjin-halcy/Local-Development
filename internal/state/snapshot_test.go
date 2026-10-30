package state

import (
	"testing"

	"github.com/erikjin-halcy/Local-Development/internal/config"
)

func TestRepoDirOverride(t *testing.T) {
	cfg := config.Config{Obsidian: config.Obsidian{Vault: "/v", ProjectDir: "10 Projekt", RepoDirs: map[string]string{"Halcy-app": "/lokal/70 devctl/Halcy-app"}}}
	if got := RepoDir(cfg, "Halcy-app"); got != "/lokal/70 devctl/Halcy-app" {
		t.Errorf("överstyrning: %s", got)
	}
	if got := RepoDir(cfg, "Dentio"); got != "/v/10 Projekt/Dentio" {
		t.Errorf("standard: %s", got)
	}
	if got := NotePath(cfg, "Halcy-app", "feat/x"); got != "/lokal/70 devctl/Halcy-app/Branches/feat ∕ x.md" {
		t.Errorf("branchanteckning: %s", got)
	}
}
