package config

// Parkerat: frågor och önskemål som kom upp medan Claude jobbade med något annat. Sparas som en
// checklista i Obsidian, per repo i <projektmapp>/<repo>/Claude/Parkerat.md, och annars i
// valvets Parkerat.md. Varje punkt har ett id (Obsidians block-id) så att den kan bockas av.

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type ParkedItem struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Date string `json:"date"`
	Done bool   `json:"done"`
	From string `json:"from,omitempty"`
}

var parkLine = regexp.MustCompile(`^- \[( |x)\] (\d{4}-\d{2}-\d{2}(?: \d{2}:\d{2})?) · (.*?)(?: \^(p[0-9a-f]{6}))?$`)

// ParkFile är listan för ett repo ("" = den allmänna listan i valvets rot).
func ParkFile(cfg Config, repoDir string) string {
	if cfg.Obsidian.Vault == "" {
		return filepath.Join(Dir(), "Parkerat.md")
	}
	if repoDir == "" {
		return filepath.Join(cfg.Obsidian.Vault, "Parkerat.md")
	}
	return filepath.Join(repoDir, "Claude", "Parkerat.md")
}

func ReadParked(path string) ([]ParkedItem, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var out []ParkedItem
	for _, l := range strings.Split(string(b), "\n") {
		if m := parkLine.FindStringSubmatch(strings.TrimRight(l, " ")); m != nil {
			it := ParkedItem{Done: m[1] == "x", Date: m[2], Text: m[3], ID: m[4]}
			if i := strings.LastIndex(it.Text, " · från "); i > 0 {
				it.From = it.Text[i+len(" · från "):]
				it.Text = it.Text[:i]
			}
			out = append(out, it)
		}
	}
	return out, nil
}

// OpenParked räknar öppna punkter.
func OpenParked(path string) int {
	items, _ := ReadParked(path)
	n := 0
	for _, it := range items {
		if !it.Done {
			n++
		}
	}
	return n
}

func newParkID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "p" + hex.EncodeToString(b)
}

// AddParked lägger till en punkt och returnerar den.
func AddParked(path, title, text, from string) (ParkedItem, error) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ParkedItem{}, fmt.Errorf("texten saknas")
	}
	it := ParkedItem{ID: newParkID(), Text: text, Date: time.Now().Format("2006-01-02 15:04"), From: strings.TrimSpace(from)}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return it, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		b = []byte(fmt.Sprintf("---\ntags: [parkerat]\n---\n# Parkerat – %s\n\nFrågor och önskemål som kom upp medan Claude jobbade med något annat. Claude tar upp dem när den är klar; bocka av när de är gjorda.\n\n", title))
	} else if err != nil {
		return it, err
	}
	line := fmt.Sprintf("- [ ] %s · %s", it.Date, it.Text)
	if it.From != "" {
		line += " · från " + it.From
	}
	line += " ^" + it.ID
	s := strings.TrimRight(string(b), "\n") + "\n" + line + "\n"
	return it, os.WriteFile(path, []byte(s), 0o644)
}

// SetParkedDone bockar av (eller öppnar) en punkt med id.
func SetParkedDone(path, id string, done bool) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	found := false
	for i, l := range lines {
		if !strings.HasSuffix(strings.TrimRight(l, " "), " ^"+id) {
			continue
		}
		found = true
		if done {
			lines[i] = strings.Replace(l, "- [ ] ", "- [x] ", 1)
		} else {
			lines[i] = strings.Replace(l, "- [x] ", "- [ ] ", 1)
		}
	}
	if !found {
		return fmt.Errorf("ingen parkerad punkt med id %s", id)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}
