package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionFileName(t *testing.T) {
	dir := t.TempDir()
	p := sessionFileName(dir, "Devctl tmux-konvention i UI", "abc")
	if filepath.Base(p) != "Devctl tmux-konvention i UI.md" {
		t.Fatalf("namn: %s", p)
	}
	_ = os.WriteFile(p, []byte("---\nsession: abc\n---\n"), 0o644)
	if sessionFileName(dir, "Devctl tmux-konvention i UI", "abc") != p {
		t.Error("samma session ska få samma fil")
	}
	if got := filepath.Base(sessionFileName(dir, "Devctl tmux-konvention i UI", "xyz")); got != "Devctl tmux-konvention i UI (2).md" {
		t.Errorf("krock: %s", got)
	}
}

func TestMergeSessionDoc(t *testing.T) {
	old := "---\nsession: abc\nuppdaterad: 1\nfeature: \"x\"\n---\n# T\n\n" + reportStart + "\n## Gjort\nmitt\n" + reportEnd + "\n\n" + autoStart + "\ngammalt\n" + autoEnd + "\n"
	fresh := "---\nsession: abc\nuppdaterad: 2\n---\n# T\n\n" + autoStart + "\nnytt\n" + autoEnd + "\n"
	got := mergeSessionDoc(old, fresh)
	for _, want := range []string{"uppdaterad: 2", "feature: \"x\"", "## Gjort\nmitt", "nytt"} {
		if !strings.Contains(got, want) {
			t.Errorf("saknar %q i\n%s", want, got)
		}
	}
	if strings.Contains(got, "gammalt") || strings.Contains(got, "uppdaterad: 1") {
		t.Errorf("gammalt kvar:\n%s", got)
	}
}
