package control

import (
	"testing"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
)

// Agenterna granskar aldrig repon med taggen local-only, varken i bakgrunden eller med ett uttryckligt repo.
func TestAgentsSkipLocalOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := config.SetTag("Hemlig-app", config.TagLocalOnly, true); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{}
	repos := []state.Repo{{Name: "Hemlig-app", Active: true}, {Name: "Öppet", Active: true}}
	a := config.Agent{Name: "sakerhet"}

	scan, skipped := agentRepos(cfg, a, repos, "")
	if len(scan) != 1 || scan[0].Name != "Öppet" || len(skipped) != 1 || skipped[0] != "Hemlig-app" {
		t.Errorf("granskar %v, hoppar över %v", scan, skipped)
	}
	if scan, _ := agentRepos(cfg, a, repos, "Hemlig-app"); len(scan) != 0 {
		t.Errorf("uttryckligt privat repo granskades: %v", scan)
	}
	c := &Controller{cfg: cfg}
	if _, err := c.agentRun(Args{"name": "sakerhet", "repo": "Hemlig-app"}); err == nil {
		t.Error("agentRun med ett local-only-repo ska nekas")
	}
}
