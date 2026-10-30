package state

import (
	"os/exec"
	"testing"
)

func TestMergedBranches(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(cmd.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("commit", "-q", "--allow-empty", "-m", "start")
	run("branch", "ny")                 // nystartad, inga egna commits
	run("checkout", "-q", "-b", "klar") // egen commit, mergad
	run("commit", "-q", "--allow-empty", "-m", "arbete")
	run("checkout", "-q", "main")
	run("merge", "-q", "--ff-only", "klar")
	run("checkout", "-q", "-b", "pågår") // egen commit, inte mergad
	run("commit", "-q", "--allow-empty", "-m", "mer")
	run("checkout", "-q", "main")

	m := mergedBranches(dir, "main")
	if !m["klar"] || m["ny"] || m["pågår"] || m["main"] {
		t.Errorf("mergade: %v (vill bara ha klar)", m)
	}
}
