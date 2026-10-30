package state

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRepoActions(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "bun.lock"), "")
	write(t, filepath.Join(root, "package.json"), `{"workspaces":["apps/*"],"scripts":{"dev":"vite","deploy:prod":"node d.js","test":"vitest","db:reset":"x"}}`)
	write(t, filepath.Join(root, "apps/mobile/package.json"), `{"scripts":{"start":"expo start"},"dependencies":{"expo":"1"}}`)
	write(t, filepath.Join(root, "Makefile"), "build:\n\tgo build\nFOO := 1\n.PHONY: build\nrelease: build\n")
	write(t, filepath.Join(root, "firebase.json"), "{}")

	got := map[string]Action{}
	for _, a := range RepoActions(root, []CustomAction{{Label: "Logga", Run: "tail -f x"}, {Label: "Deploy staging", Run: "./d.sh", Group: "Deploy", Danger: true}}) {
		got[a.ID] = a
	}
	want := map[string]struct {
		group, run string
		danger     bool
	}{
		"npm::dev":               {"Starta", "bun run dev", false},
		"npm::deploy:prod":       {"Deploy", "bun run deploy:prod", true},
		"npm::test":              {"Kontroll", "bun run test", false},
		"npm::db:reset":          {"Databas", "bun run db:reset", true},
		"npm:apps/mobile:start":  {"Starta", "bun run start", false},
		"make:build":             {"Bygg", "make build", false},
		"make:release":           {"Deploy", "make release", true},
		"detected:firebase.json": {"Deploy", "npx firebase-tools deploy", true},
		"custom:Logga":           {"Egna", "tail -f x", false},
		"custom:Deploy staging":  {"Deploy", "./d.sh", true},
	}
	for id, w := range want {
		a, ok := got[id]
		if !ok {
			t.Errorf("saknar %s (fick %v)", id, keys(got))
			continue
		}
		if a.Group != w.group || a.Run != w.run || a.Danger != w.danger {
			t.Errorf("%s: fick %s/%q/danger=%v, vill ha %s/%q/%v", id, a.Group, a.Run, a.Danger, w.group, w.run, w.danger)
		}
	}
	if _, ok := got["make:FOO"]; ok {
		t.Error("variabeltilldelning ska inte bli ett make-mål")
	}
	if a := got["npm:apps/mobile:start"]; a.Label != "Metro (start) · apps/mobile" || a.Dir != "apps/mobile" {
		t.Errorf("Metro: %+v", a)
	}
}

func keys(m map[string]Action) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
