package config

import (
	"os"
	"strings"
	"testing"
)

func TestTodos(t *testing.T) {
	cfg := Config{Obsidian: Obsidian{Vault: t.TempDir()}}
	t.Setenv("HOME", t.TempDir()) // CacheDir under en tillfällig hemkatalog
	a, err := AddTodo(cfg, "Lägg till dark mode", "devctl", "Mörkt tema i appen.", "Kom upp under arbete med historiken.", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddTodo(cfg, "Andra uppgiften", "", "", "", "test"); err != nil {
		t.Fatal(err)
	}
	page, _ := os.ReadFile(a.File)
	for _, want := range []string{"status: öppen", "## Vad\nMörkt tema i appen.", "## Bakgrund\nKom upp", "[[devctl]]"} {
		if !strings.Contains(string(page), want) {
			t.Errorf("sidan saknar %q", want)
		}
	}
	idx, _ := os.ReadFile(TodoIndex(cfg))
	if !strings.Contains(string(idx), "## Att göra (2)") || !strings.Contains(string(idx), "|Lägg till dark mode]]") {
		t.Fatalf("listan:\n%s", idx)
	}
	// Bocka av i listan och lägg till en egen rad utanför devctl-delen
	s := strings.Replace(string(idx), "- [ ] [["+strings.TrimSuffix(a.File[strings.LastIndex(a.File, "/")+1:], ".md"), "- [x] [["+strings.TrimSuffix(a.File[strings.LastIndex(a.File, "/")+1:], ".md"), 1)
	s += "\nMin egen anteckning\n"
	if err := os.WriteFile(TodoIndex(cfg), []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SyncTodos(cfg); err != nil {
		t.Fatal(err)
	}
	for _, td := range Todos(cfg) {
		if td.ID == a.ID && (td.Status != "klar" || td.Done == "") {
			t.Errorf("avbockning fördes inte över: %+v", td)
		}
	}
	idx, _ = os.ReadFile(TodoIndex(cfg))
	if !strings.Contains(string(idx), "## Att göra (1)") || !strings.Contains(string(idx), "Min egen anteckning") || !strings.Contains(string(idx), "- [x] [[") {
		t.Errorf("listan efter synk:\n%s", idx)
	}
	// devctl todo done på den andra: listan har inte ändrats av användaren, så den får inte öppna den igen
	var b Todo
	for _, td := range Todos(cfg) {
		if td.Title == "Andra uppgiften" {
			b = td
		}
	}
	if _, err := SetTodoStatus(cfg, b.ID, "klar"); err != nil {
		t.Fatal(err)
	}
	for _, td := range Todos(cfg) {
		if td.ID == b.ID && td.Status != "klar" {
			t.Errorf("todo done återställdes av listan: %+v", td)
		}
	}
	if OpenTodos(cfg)["devctl"] != 0 || OpenTodos(cfg)[""] != 0 {
		t.Errorf("OpenTodos = %v", OpenTodos(cfg))
	}
}
