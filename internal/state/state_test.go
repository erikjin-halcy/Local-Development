package state

import (
	"testing"

	"github.com/erikjin-halcy/Local-Development/internal/config"
)

func TestParseTrack(t *testing.T) {
	for in, want := range map[string][3]int{"": {0, 0, 0}, "ahead 3": {3, 0, 0}, "behind 2": {0, 2, 0}, "ahead 1, behind 4": {1, 4, 0}, "gone": {0, 0, 1}} {
		a, b, g := parseTrack(in)
		gi := 0
		if g {
			gi = 1
		}
		if [3]int{a, b, gi} != want {
			t.Errorf("parseTrack(%q) = %d,%d,%v; vill ha %v", in, a, b, g, want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		s    Server
		want string
		app  bool
	}{
		{Server{Process: "node", Command: "node /x/node_modules/.bin/vite --port 5173"}, "frontend", false},
		{Server{Process: "python3.12", Command: ".venv/bin/python -m uvicorn app.main:app"}, "backend", false},
		{Server{Process: "postgres", Command: "/opt/homebrew/opt/postgresql@16/bin/postgres -D x"}, "database", false},
		{Server{Process: "OrbStack Helper", Command: "/Applications/OrbStack.app/x"}, "docker", false},
		{Server{Process: "WeChat", Command: "/Applications/WeChat.app/Contents/MacOS/WeChat", Cwd: "/Users/x/Library"}, "app", true},
		{Server{Process: "node", Command: "node index.js", Cwd: "/Users/x/repo", Worktree: &Ref{Repo: "r"}}, "dev", false},
	}
	for _, c := range cases {
		s := c.s
		classify(&s)
		if s.Kind != c.want || s.App != c.app {
			t.Errorf("%s: fick %s/app=%v, vill ha %s/app=%v", c.s.Command, s.Kind, s.App, c.want, c.app)
		}
	}
}

func TestNotePath(t *testing.T) {
	cfg := config.Config{Obsidian: config.Obsidian{Vault: "/v", ProjectDir: "Projekt"}}
	if got := NotePath(cfg, "repo", "feat/x"); got != "/v/Projekt/repo/Branches/feat ∕ x.md" {
		t.Errorf("NotePath = %q", got)
	}
	if got := RepoNotePath(cfg, "Halcy-app"); got != "/v/Projekt/Halcy-app/Halcy-app.md" {
		t.Errorf("RepoNotePath = %q", got)
	}
	if NotePath(cfg, "repo", "(abc123)") != "" {
		t.Error("detached HEAD ska inte få någon anteckning")
	}
}
