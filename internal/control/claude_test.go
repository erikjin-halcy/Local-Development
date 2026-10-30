package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erikjin-halcy/Local-Development/internal/state"
)

func TestParseSession(t *testing.T) {
	lines := []string{
		`{"type":"user","sessionId":"abc12345-x","cwd":"/Users/x/Desktop","gitBranch":"main","timestamp":"2026-10-30T10:00:00Z","message":{"role":"user","content":"Bygg en kör-meny"}}`,
		`{"type":"user","sessionId":"abc12345-x","timestamp":"2026-10-30T10:00:01Z","message":{"role":"user","content":"<system-reminder>brus</system-reminder>"}}`,
		`{"type":"user","sessionId":"abc12345-x","timestamp":"2026-10-30T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"assistant","sessionId":"abc12345-x","timestamp":"2026-10-30T10:05:00Z","message":{"model":"claude-opus-5-5","content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/repo/a.go"}},{"type":"tool_use","name":"Write","input":{"file_path":"/repo/b.go"}},{"type":"tool_use","name":"Bash","input":{"command":"go test"}},{"type":"text","text":"Klart."}]}}`,
		`{"type":"ai-title","sessionId":"abc12345-x","aiTitle":"Kör-meny i devctl"}`,
		`{"type":"api-request","sessionId":"abc12345-x","params":{"x":1}}`,
	}
	path := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := parseSession(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Title != "Kör-meny i devctl" || len(s.Prompts) != 1 || s.Prompts[0] != "Bygg en kör-meny" || s.Commands != 1 || s.Reply != "Klart." || s.Branch != "main" {
		t.Errorf("fel tolkning: %+v", s)
	}
	// Startad på skrivbordet men ändrar filer i /repo: hör till det repot
	snap := state.Snapshot{Repos: []state.Repo{{Name: "repo", Worktrees: []state.Worktree{{Path: "/repo"}}}}}
	if r, wt := repoForSession(snap, s); r == nil || r.Name != "repo" || wt != "/repo" {
		t.Errorf("repoForSession = %v %q", r, wt)
	}
}
