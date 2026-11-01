package state

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestWorktreeStage(t *testing.T) {
	cases := []struct {
		name string
		w    Worktree
		want string
	}{
		{"huvudbranchen", Worktree{Main: true, Branch: "main", Changes: 2}, ""},
		{"huvudkatalogen på en annan branch", Worktree{Main: true, Branch: "b", Changes: 2}, StageChanges},
		{"detached", Worktree{Head: "abc"}, ""},
		{"ny", Worktree{Branch: "b"}, StageFresh},
		{"ändringar", Worktree{Branch: "b", Changes: 1, Base: 3, PR: &PR{}}, StageChanges},
		{"lokal commit", Worktree{Branch: "b", Base: 1}, StageCommitted},
		{"opushad commit", Worktree{Branch: "b", Base: 2, Upstream: "origin/b", Ahead: 1}, StageCommitted},
		{"pushad", Worktree{Branch: "b", Base: 2, Upstream: "origin/b"}, StagePushed},
		{"pr", Worktree{Branch: "b", Base: 2, Upstream: "origin/b", PR: &PR{Number: 4}}, StagePR},
		{"landad", Worktree{Branch: "b", Landed: true}, StageLanded},
	}
	for _, c := range cases {
		if got := WorktreeStage(c.w, "main"); got != c.want {
			t.Errorf("%s: fick %q, vill ha %q", c.name, got, c.want)
		}
	}
}

func TestIssueKey(t *testing.T) {
	for in, want := range map[string]string{
		"erik/hal-123-ny-meny": "HAL-123", "HAL-7": "HAL-7", "feat/hal-12": "HAL-12",
		"feat/halo-12-x": "", "feat/hal-": "", "main": "",
	} {
		if got := IssueKey(in, "HAL"); got != want {
			t.Errorf("IssueKey(%q) = %q; vill ha %q", in, got, want)
		}
	}
	if IssueKey("hal-1", "") != "" {
		t.Error("utan team ska inget ärende hittas")
	}
}

func TestRepoSkills(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		d := filepath.Join(root, ".claude", "skills", name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("verify", "---\nname: verify\ndescription: \"Verifiera: appen i simulatorn.\"\n---\n\nText")
	write("utan-namn", "---\ndescription: Bara en beskrivning\n---\n")
	write("trasig", "ingen frontmatter")
	got := RepoSkills(root)
	if len(got) != 3 || got[0].Name != "trasig" || got[1].Name != "utan-namn" || got[2] != (Skill{"verify", "Verifiera: appen i simulatorn."}) {
		t.Fatalf("RepoSkills = %+v", got)
	}
	// Ny skill läses in när katalogen ändras
	time.Sleep(10 * time.Millisecond)
	write("commit", "---\nname: commit\ndescription: Committa\n---\n")
	if got := RepoSkills(root); len(got) != 4 || got[0].Name != "commit" {
		t.Fatalf("efter ändring: %+v", got)
	}
	if got := RepoSkills(t.TempDir()); len(got) != 0 {
		t.Fatalf("repo utan skills: %+v", got)
	}
}

func TestBaseStatus(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "start")
	run("branch", "ny")
	run("checkout", "-q", "-b", "jobb")
	run("commit", "-q", "--allow-empty", "-m", "jobb 1")
	run("checkout", "-q", "main")
	wts := []Worktree{{Main: true, Branch: "main", Base: -1}, {Branch: "ny"}, {Branch: "jobb"}}
	baseStatus(dir, "main", wts)
	if wts[0].Base != -1 {
		t.Fatal("huvudbranchen ska inte räknas")
	}
	if wts[1].Base != 0 || wts[1].Landed || wts[2].Base != 1 || wts[2].Landed {
		t.Fatalf("före landning: %+v", wts)
	}
	run("merge", "-q", "--ff-only", "jobb")
	wts = []Worktree{{Main: true, Branch: "main"}, {Branch: "ny"}, {Branch: "jobb"}}
	baseStatus(dir, "main", wts)
	if wts[1].Landed || wts[2].Base != 0 || !wts[2].Landed {
		t.Fatalf("efter landning: %+v", wts)
	}
}
