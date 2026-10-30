package control

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
)

// Ett privat repo (obsidian.repoDirs eller cloud.exclude) får aldrig komma med i molnsammanfattningen.
func TestCloudSummaryExcludesPrivateRepos(t *testing.T) {
	cfg := config.Config{
		Obsidian: config.Obsidian{RepoDirs: map[string]string{"Hemlig-app": "/lokal/Hemlig-app"}},
	}
	t.Setenv("HOME", t.TempDir())
	if err := config.SetTag("intern-firma", config.TagLocalOnly, true); err != nil {
		t.Fatal(err)
	}
	wt := func(repo, path, branch string) state.Worktree {
		return state.Worktree{Path: path, Branch: branch, Panes: []string{}, Servers: []int{}, Containers: []string{}}
	}
	snap := state.Snapshot{Repos: []state.Repo{
		{Name: "Hemlig-app", Path: "/r/Hemlig-app", Active: true, Worktrees: []state.Worktree{wt("Hemlig-app", "/r/Hemlig-app.worktrees/x", "feat/hemlig-branch")}},
		{Name: "intern-firma", Path: "/r/intern-firma", Active: true, Worktrees: []state.Worktree{wt("intern-firma", "/r/intern-firma", "main")}},
		{Name: "Öppet", Path: "/r/Öppet", Active: true, Worktrees: []state.Worktree{wt("Öppet", "/r/Öppet", "main")}},
	}}
	now := time.Now().Format("2006-01-02 15:04")
	infos := []sessionInfo{
		{Name: "s1", Repo: "Hemlig-app", Branch: "feat/hemlig-branch", Title: "Hemlig titel", Last: now, Prompt: "hemligt önskemål"},
		{Name: "s2", Cwd: "/r/Hemlig-app.worktrees/x", Title: "utan repo", Last: now},
		{Name: "s3", Repo: "Öppet", Title: "Öppen session", Last: now, Prompt: "fixa Hemlig-app:s inloggning", Reply: "klart"},
	}
	fs := []cloudFinding{
		{Agent: "🛡 Säkerhet", F: Finding{ID: "f1", Status: "öppen", Repo: "Hemlig-app", Branch: "feat/hemlig-branch", Finding: "nyckel i koden"}},
		{Agent: "🛡 Säkerhet", F: Finding{ID: "f2", Status: "öppen", Repo: "Öppet", Finding: "öppet fynd"}},
	}
	todos := []config.Todo{{Title: "privat uppgift", Repo: "intern-firma", Status: "öppen"}, {Title: "öppen uppgift", Repo: "Öppet", Status: "öppen"}}

	out := cloudSummaryOf(cfg, snap, infos, fs, todos)
	b, _ := json.Marshal(out)
	doc := strings.ToLower(string(b))
	for _, secret := range []string{"hemlig", "intern-firma", "privat uppgift", "nyckel i koden"} {
		if strings.Contains(doc, secret) {
			t.Errorf("%q finns i sammanfattningen:\n%s", secret, b)
		}
	}
	for _, want := range []string{"öppen session", "öppet fynd", "öppen uppgift", `"klart"`} {
		if !strings.Contains(doc, want) {
			t.Errorf("%q saknas", want)
		}
	}
	p := out["private"].(map[string]any)
	if p["repos"] != 2 || p["sessions"] != 2 || p["findings"] != 1 || p["todos"] != 1 {
		t.Errorf("antal: %v", p)
	}
}
