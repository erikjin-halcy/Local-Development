package state

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

const sep = "\x1f"

type Pane struct {
	ID      string `json:"id"`
	Index   int    `json:"index"`
	Title   string `json:"title"`
	Active  bool   `json:"active"`
	Command string `json:"command"`
	Path    string `json:"path"`
	Left    int    `json:"left"`
	Top     int    `json:"top"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Role    string `json:"role"`
	// Namn som användaren gett panen (@name); visas i stället för programmet
	Name     string `json:"name,omitempty"`
	Dead     bool   `json:"dead"`
	PID      int    `json:"pid"`
	Worktree *Ref   `json:"worktree,omitempty"`
	// Servrar som startats från denna pane (portar)
	Ports []int `json:"ports,omitempty"`
}

type Window struct {
	ID      string `json:"id"`
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Active  bool   `json:"active"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	Kind    string `json:"kind"`
	Zoomed  bool   `json:"zoomed"`
	Parent  string `json:"parent"`
	Panes   []Pane `json:"panes"`
	Session string `json:"session"`
}

type Session struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Attached int      `json:"attached"`
	Windows  []Window `json:"windows"`
}

// Ref pekar ut en worktree (och därmed en branch) i ett repo.
type Ref struct {
	Repo   string `json:"repo"`
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

func Tmux(args ...string) (string, error) {
	out, err := sysx.Run(10*time.Second, "tmux", args...)
	if err != nil && (strings.Contains(err.Error(), "no server running") || strings.Contains(err.Error(), "error connecting")) {
		return "", nil
	}
	return out, err
}

func fmtFields(fields ...string) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = "#{" + f + "}"
	}
	return strings.Join(parts, sep)
}

func rows(args ...string) ([][]string, error) {
	out, err := Tmux(args...)
	if err != nil {
		return nil, err
	}
	var res [][]string
	for _, l := range strings.Split(out, "\n") {
		if l != "" {
			res = append(res, strings.Split(l, sep))
		}
	}
	return res, nil
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func collectTmux() ([]Session, error) {
	sessRows, err := rows("list-sessions", "-F", fmtFields("session_id", "session_name", "session_attached"))
	if err != nil {
		return nil, err
	}
	sessions := map[string]*Session{}
	var order []*Session
	for _, r := range sessRows {
		if len(r) < 3 {
			continue
		}
		s := &Session{ID: r[0], Name: r[1], Attached: atoi(r[2])}
		sessions[r[0]] = s
		order = append(order, s)
	}
	winRows, err := rows("list-windows", "-a", "-F", fmtFields("session_id", "window_id", "window_index", "window_name",
		"window_active", "window_width", "window_height", "@kind", "window_zoomed_flag", "@parent"))
	if err != nil {
		return nil, err
	}
	windows := map[string]*Window{}
	var winOrder []*Window
	for _, r := range winRows {
		if len(r) < 10 {
			continue
		}
		w := &Window{Session: r[0], ID: r[1], Index: atoi(r[2]), Name: r[3], Active: r[4] == "1",
			Width: atoi(r[5]), Height: atoi(r[6]), Kind: r[7], Zoomed: r[8] == "1", Parent: r[9], Panes: []Pane{}}
		windows[w.ID] = w
		winOrder = append(winOrder, w)
	}
	paneRows, err := rows("list-panes", "-a", "-F", fmtFields("window_id", "pane_id", "pane_index", "pane_title",
		"pane_active", "pane_current_command", "pane_current_path", "pane_left", "pane_top", "pane_width",
		"pane_height", "@role", "pane_dead", "pane_pid", "@name"))
	if err != nil {
		return nil, err
	}
	for _, r := range paneRows {
		if len(r) < 15 {
			continue
		}
		if w := windows[r[0]]; w != nil {
			w.Panes = append(w.Panes, Pane{ID: r[1], Index: atoi(r[2]), Title: r[3], Active: r[4] == "1",
				Command: r[5], Path: r[6], Left: atoi(r[7]), Top: atoi(r[8]), Width: atoi(r[9]),
				Height: atoi(r[10]), Role: r[11], Dead: r[12] == "1", PID: atoi(r[13]), Name: r[14]})
		}
	}
	for _, w := range winOrder {
		if s := sessions[w.Session]; s != nil {
			s.Windows = append(s.Windows, *w)
		}
	}
	out := make([]Session, 0, len(order))
	for _, s := range order {
		ids := map[string]bool{}
		for _, w := range s.Windows {
			ids[w.ID] = true
		}
		// Förälder som stängts eller ligger i en annan session => rotnivå
		for i := range s.Windows {
			if !ids[s.Windows[i].Parent] || s.Windows[i].Parent == s.Windows[i].ID {
				s.Windows[i].Parent = ""
			}
		}
		sort.Slice(s.Windows, func(i, j int) bool { return s.Windows[i].Index < s.Windows[j].Index })
		if s.Windows == nil {
			s.Windows = []Window{}
		}
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func itoa(n int) string { return strconv.Itoa(n) }
