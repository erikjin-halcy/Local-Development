package config

// To do i Obsidian-valvet: en mapp med en sida per uppgift (med kontext) och en lista, To do.md, som
// devctl håller aktuell. Sidornas frontmatter är källan; en kryssruta som bockas av i listan förs
// över till sidan vid nästa synk.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Todo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"` // öppen, pågår, klar
	Repo    string `json:"repo,omitempty"`
	Created string `json:"created"`
	Done    string `json:"done,omitempty"`
	Source  string `json:"source,omitempty"`
	File    string `json:"file"`
}

const todoFolder = "05 To do"

// TodoDir är mappen i valvet (eller i devctl:s mapp om inget valv är valt).
func TodoDir(cfg Config) string {
	if cfg.Obsidian.Vault == "" {
		return filepath.Join(Dir(), "todo")
	}
	return filepath.Join(cfg.Obsidian.Vault, todoFolder)
}

func TodoIndex(cfg Config) string { return filepath.Join(TodoDir(cfg), "To do.md") }

var fmLine = regexp.MustCompile(`^([a-zåäö]+):\s?(.*)$`)

func readTodo(path string) (Todo, bool) {
	b, err := os.ReadFile(path)
	if err != nil || !strings.HasPrefix(string(b), "---\n") {
		return Todo{}, false
	}
	end := strings.Index(string(b)[4:], "\n---")
	if end < 0 {
		return Todo{}, false
	}
	t := Todo{File: path}
	for _, l := range strings.Split(string(b)[4:4+end], "\n") {
		if m := fmLine.FindStringSubmatch(l); m != nil {
			v := strings.Trim(strings.TrimSpace(m[2]), `"`)
			switch m[1] {
			case "id":
				t.ID = v
			case "status":
				t.Status = v
			case "repo":
				t.Repo = v
			case "skapad":
				t.Created = v
			case "klar":
				t.Done = v
			case "källa":
				t.Source = v
			case "titel":
				t.Title = v
			}
		}
	}
	if t.ID == "" {
		return Todo{}, false
	}
	if t.Title == "" {
		t.Title = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	if t.Status == "" {
		t.Status = "öppen"
	}
	return t, true
}

// Todos läser alla uppgifter, öppna först (äldst först), sedan klara (senast klara först).
func Todos(cfg Config) []Todo {
	files, _ := filepath.Glob(filepath.Join(TodoDir(cfg), "*.md"))
	var out []Todo
	for _, f := range files {
		if filepath.Base(f) == "To do.md" {
			continue
		}
		if t, ok := readTodo(f); ok {
			out = append(out, t)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		di, dj := out[i].Status == "klar", out[j].Status == "klar"
		if di != dj {
			return !di
		}
		if di {
			return out[i].Done > out[j].Done
		}
		return out[i].Created < out[j].Created
	})
	return out
}

var unsafeTitle = regexp.MustCompile(`[/\\:*?"<>|#^\[\]\n\r\t]+`)

// AddTodo skapar en uppgift med en egen sida och uppdaterar listan.
func AddTodo(cfg Config, title, repo, context, background, source string) (Todo, error) {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return Todo{}, fmt.Errorf("titeln saknas")
	}
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	now := time.Now()
	t := Todo{ID: "t" + hex.EncodeToString(b), Title: title, Status: "öppen", Repo: repo, Created: now.Format("2006-01-02 15:04"), Source: source}
	name := strings.Join(strings.Fields(unsafeTitle.ReplaceAllString(title, " ")), " ")
	if r := []rune(name); len(r) > 70 {
		name = strings.TrimSpace(string(r[:70]))
	}
	dir := TodoDir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return t, err
	}
	t.File = filepath.Join(dir, now.Format("2006-01-02")+" "+name+".md")
	for i := 2; ; i++ {
		if _, err := os.Stat(t.File); os.IsNotExist(err) {
			break
		}
		t.File = filepath.Join(dir, fmt.Sprintf("%s %s (%d).md", now.Format("2006-01-02"), name, i))
	}
	if context == "" {
		context = title
	}
	repoLine := ""
	if repo != "" {
		repoLine = "\n**Repo:** [[" + repo + "]]"
	}
	if background == "" {
		background = "_Ingen bakgrund angiven._"
	}
	page := fmt.Sprintf(`---
id: %s
titel: %q
status: öppen
repo: %s
skapad: %s
källa: %q
tags: [todo]
---
# %s
%s
[[To do|← To do-listan]]

## Vad
%s

## Bakgrund
%s

## Nästa steg
- [ ]

## Anteckningar

`, t.ID, title, repo, t.Created, source, title, repoLine, strings.TrimSpace(context), strings.TrimSpace(background))
	if err := os.WriteFile(t.File, []byte(page), 0o644); err != nil {
		return t, err
	}
	return t, SyncTodos(cfg)
}

// SetTodoStatus sätter status (öppen, pågår, klar) på sidan och uppdaterar listan.
func SetTodoStatus(cfg Config, id, status string) (Todo, error) {
	for _, t := range Todos(cfg) {
		if t.ID == id || strings.EqualFold(t.Title, id) {
			if err := writeStatus(t.File, status); err != nil {
				return t, err
			}
			t.Status = status
			return t, SyncTodos(cfg)
		}
	}
	return Todo{}, fmt.Errorf("ingen uppgift med id %s", id)
}

func writeStatus(path, status string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	end := strings.Index(s[4:], "\n---") + 4
	fm := s[:end]
	lines := strings.Split(fm, "\n")
	var out []string
	for _, l := range lines {
		if strings.HasPrefix(l, "status:") || strings.HasPrefix(l, "klar:") {
			continue
		}
		out = append(out, l)
		if strings.HasPrefix(l, "id:") {
			out = append(out, "status: "+status)
			if status == "klar" {
				out = append(out, "klar: "+time.Now().Format("2006-01-02 15:04"))
			}
		}
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")+s[end:]), 0o644)
}

const todoStart, todoEnd = "<!-- devctl:todo:start -->", "<!-- devctl:todo:end -->"

var indexItem = regexp.MustCompile(`^- \[( |x|X|/)\] \[\[([^\]|]+)(?:\|[^\]]*)?\]\]`)

// SyncTodos för över avbockningar från listan till sidorna och skriver om listan.
func SyncTodos(cfg Config) error {
	index := TodoIndex(cfg)
	todos := Todos(cfg)
	byName := map[string]Todo{}
	for _, t := range todos {
		byName[strings.TrimSuffix(filepath.Base(t.File), ".md")] = t
	}
	old, _ := os.ReadFile(index)
	// Bara ändringar som användaren gjort i listan förs över: jämför med det devctl senast skrev
	lastPath := filepath.Join(CacheDir(), "todo-index.md")
	last, _ := os.ReadFile(lastPath)
	current := ""
	if i, j := strings.Index(string(old), todoStart), strings.Index(string(old), todoEnd); i >= 0 && j > i {
		current = string(old)[i : j+len(todoEnd)]
	}
	userEdited := current != "" && current != string(last)
	changed := false
	for _, l := range strings.Split(current, "\n") {
		if !userEdited {
			break
		}
		m := indexItem.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		t, ok := byName[m[2]]
		if !ok {
			continue
		}
		want := map[string]string{" ": "", "x": "klar", "X": "klar", "/": "pågår"}[m[1]]
		switch {
		case want == "klar" && t.Status != "klar":
			_ = writeStatus(t.File, "klar")
			changed = true
		case want == "" && t.Status == "klar":
			_ = writeStatus(t.File, "öppen")
			changed = true
		case want == "pågår" && t.Status == "öppen":
			_ = writeStatus(t.File, "pågår")
			changed = true
		}
	}
	if changed {
		todos = Todos(cfg)
	}

	var b strings.Builder
	b.WriteString(todoStart + "\n")
	open, done := 0, 0
	for _, t := range todos {
		if t.Status != "klar" {
			open++
		}
	}
	fmt.Fprintf(&b, "## Att göra (%d)\n", open)
	for _, t := range todos {
		if t.Status == "klar" {
			continue
		}
		box := " "
		if t.Status == "pågår" {
			box = "/"
		}
		fmt.Fprintf(&b, "- [%s] [[%s|%s]]", box, strings.TrimSuffix(filepath.Base(t.File), ".md"), t.Title)
		if t.Repo != "" {
			fmt.Fprintf(&b, " · [[%s]]", t.Repo)
		}
		fmt.Fprintf(&b, " · %s\n", t.Created[:min(10, len(t.Created))])
	}
	if open == 0 {
		b.WriteString("_Inget att göra._\n")
	}
	b.WriteString("\n## Klart (senaste 20)\n")
	for _, t := range todos {
		if t.Status != "klar" || done == 20 {
			continue
		}
		done++
		fmt.Fprintf(&b, "- [x] [[%s|%s]] · klar %s\n", strings.TrimSuffix(filepath.Base(t.File), ".md"), t.Title, t.Done[:min(10, len(t.Done))])
	}
	if done == 0 {
		b.WriteString("_Inget än._\n")
	}
	b.WriteString(todoEnd)
	section := b.String()
	_ = os.WriteFile(lastPath, []byte(section), 0o644)

	var out string
	s := string(old)
	if strings.Contains(s, todoStart) && strings.Contains(s, todoEnd) {
		i, j := strings.Index(s, todoStart), strings.Index(s, todoEnd)+len(todoEnd)
		out = s[:i] + section + s[j:]
	} else {
		out = "---\ntags: [todo]\n---\n# To do\n\nBocka av här eller sätt `status: klar` på sidan. `[/]` betyder pågår. Varje uppgift har en egen sida med kontext; devctl håller listan nedan aktuell (det du skriver utanför den rörs inte).\n\n" + section + "\n"
	}
	if out == s {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(index), 0o755); err != nil {
		return err
	}
	return os.WriteFile(index, []byte(out), 0o644)
}

// OpenTodos räknar öppna uppgifter (öppen eller pågår) per repo ("" = utan repo).
func OpenTodos(cfg Config) map[string]int {
	out := map[string]int{}
	for _, t := range Todos(cfg) {
		if t.Status != "klar" {
			out[t.Repo]++
		}
	}
	return out
}
