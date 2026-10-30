package state

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/erikjin-halcy/Local-Development/internal/config"
)

// Action är något man kan köra i en worktree: ett package.json-skript, ett make-/just-mål, ett
// deploy-kommando som känns igen från repots filer, eller en egen action (~/.config/devctl/actions/<repo>.yaml).
type Action struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Group  string `json:"group"`
	Dir    string `json:"dir"`    // underkatalog i worktreen ("" = roten)
	Run    string `json:"run"`    // kommandoraden
	Danger bool   `json:"danger"` // kräver bekräftelse (deploy, migrering, borttagning …)
	Source string `json:"source"` // npm, make, just, custom, detected
}

type CustomAction = config.CustomAction

// Grupper i den ordning de visas
var GroupOrder = []string{"Egna", "Starta", "Deploy", "Bygg", "Kontroll", "Databas", "Övrigt"}

var (
	dangerRe = regexp.MustCompile(`(?i)deploy|release|publish|\bprod|migrat|\bpush\b|delete|destroy|drop|reset|distribute|rollback|seed`)
	makeRe   = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9_.-]*)\s*:([^=]|$)`)
	justRe   = regexp.MustCompile(`^@?([A-Za-z0-9][A-Za-z0-9_-]*)(\s+[^:=]*)?:([^=]|$)`)
)

func group(name, run string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "deploy") || strings.Contains(n, "release") || strings.Contains(n, "publish") || strings.Contains(n, "distribute"):
		return "Deploy"
	case n == "dev" || n == "start" || n == "serve" || n == "watch" || strings.HasPrefix(n, "dev:") || strings.HasPrefix(n, "dev-") ||
		strings.HasSuffix(n, "-dev") || strings.HasPrefix(n, "start:") || strings.HasPrefix(n, "serve") || n == "preview" || strings.HasSuffix(n, "-preview"):
		return "Starta"
	case strings.Contains(n, "build") || strings.HasPrefix(n, "gen") || strings.Contains(n, "compile") || strings.Contains(n, "bundle"):
		return "Bygg"
	case strings.Contains(n, "test") || strings.Contains(n, "lint") || strings.Contains(n, "check") || strings.Contains(n, "format") ||
		strings.Contains(n, "typecheck") || strings.Contains(n, "verify") || strings.Contains(n, "type-check"):
		return "Kontroll"
	case strings.HasPrefix(n, "db") || strings.Contains(n, "migrat") || strings.Contains(n, "supabase") || strings.Contains(n, "prisma"):
		return "Databas"
	}
	if strings.Contains(strings.ToLower(run), "deploy") {
		return "Deploy"
	}
	return "Övrigt"
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	return err == nil && json.Unmarshal(b, v) == nil
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

// packageManager väljer kommando efter låsfilen i repots rot.
func packageManager(root string) string {
	switch {
	case exists(filepath.Join(root, "bun.lock")) || exists(filepath.Join(root, "bun.lockb")):
		return "bun run"
	case exists(filepath.Join(root, "pnpm-lock.yaml")):
		return "pnpm run"
	case exists(filepath.Join(root, "yarn.lock")):
		return "yarn"
	}
	return "npm run"
}

type pkgJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Workspaces      any               `json:"workspaces"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

func (p pkgJSON) has(dep string) bool {
	_, a := p.Dependencies[dep]
	_, b := p.DevDependencies[dep]
	return a || b
}

func workspaceDirs(root string, p pkgJSON) []string {
	var patterns []string
	switch w := p.Workspaces.(type) {
	case []any:
		for _, x := range w {
			if s, ok := x.(string); ok {
				patterns = append(patterns, s)
			}
		}
	case map[string]any: // yarn: {"packages": [...]}
		if list, ok := w["packages"].([]any); ok {
			for _, x := range list {
				if s, ok := x.(string); ok {
					patterns = append(patterns, s)
				}
			}
		}
	}
	var dirs []string
	for _, pat := range patterns {
		matches, _ := filepath.Glob(filepath.Join(root, pat))
		for _, m := range matches {
			if exists(filepath.Join(m, "package.json")) {
				rel, _ := filepath.Rel(root, m)
				dirs = append(dirs, rel)
			}
		}
	}
	sort.Strings(dirs)
	return dirs
}

func scriptActions(root, dir string, pm string) []Action {
	var p pkgJSON
	if !readJSON(filepath.Join(root, dir, "package.json"), &p) {
		return nil
	}
	mobile := p.has("expo") || p.has("react-native")
	names := make([]string, 0, len(p.Scripts))
	for n := range p.Scripts {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []Action
	for _, n := range names {
		run := p.Scripts[n]
		a := Action{ID: "npm:" + dir + ":" + n, Label: n, Dir: dir, Run: pm + " " + n, Source: "npm",
			Group: group(n, run), Danger: dangerRe.MatchString(n) || dangerRe.MatchString(run)}
		// Metro: start-skriptet i en Expo/React Native-app
		if mobile && (n == "start" || n == "dev") {
			a.Label = "Metro (" + n + ")"
			a.Group = "Starta"
		}
		if dir != "" {
			a.Label += " · " + dir
		}
		out = append(out, a)
	}
	return out
}

func targets(path string, re *regexp.Regexp, limit int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() && len(out) < limit {
		if m := re.FindStringSubmatch(sc.Text()); m != nil && !strings.HasPrefix(m[1], ".") && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// RepoActions tar fram alla actions för en worktree. custom är repots egna actions.
func RepoActions(root string, custom []CustomAction) []Action {
	var out []Action
	for _, c := range custom {
		if c.Label == "" || c.Run == "" {
			continue
		}
		g := c.Group
		if g == "" {
			g = "Egna"
		}
		out = append(out, Action{ID: "custom:" + c.Label, Label: c.Label, Group: g, Dir: c.Dir, Run: c.Run,
			Danger: c.Danger || dangerRe.MatchString(c.Label), Source: "custom"})
	}

	pm := packageManager(root)
	out = append(out, scriptActions(root, "", pm)...)
	var p pkgJSON
	if readJSON(filepath.Join(root, "package.json"), &p) {
		for _, d := range workspaceDirs(root, p) {
			out = append(out, scriptActions(root, d, pm)...)
		}
	}
	for _, t := range targets(filepath.Join(root, "Makefile"), makeRe, 60) {
		out = append(out, Action{ID: "make:" + t, Label: "make " + t, Run: "make " + t, Source: "make",
			Group: group(t, ""), Danger: dangerRe.MatchString(t)})
	}
	for _, name := range []string{"justfile", "Justfile", ".justfile"} {
		for _, t := range targets(filepath.Join(root, name), justRe, 60) {
			out = append(out, Action{ID: "just:" + t, Label: "just " + t, Run: "just " + t, Source: "just",
				Group: group(t, ""), Danger: dangerRe.MatchString(t)})
		}
	}
	for _, d := range []struct{ file, label, run string }{
		{"firebase.json", "Firebase deploy", "npx firebase-tools deploy"},
		{"fly.toml", "Fly deploy", "fly deploy"},
		{"vercel.json", "Vercel deploy", "npx vercel deploy"},
	} {
		if exists(filepath.Join(root, d.file)) {
			out = append(out, Action{ID: "detected:" + d.file, Label: d.label, Run: d.run, Group: "Deploy", Danger: true, Source: "detected"})
		}
	}
	rank := map[string]int{}
	for i, g := range GroupOrder {
		rank[g] = i
	}
	sort.SliceStable(out, func(i, j int) bool { return rank[out[i].Group] < rank[out[j].Group] })
	if out == nil {
		out = []Action{}
	}
	return out
}
