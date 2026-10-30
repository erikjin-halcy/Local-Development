package control

// Kommandon som gör orkestratorn "native" i devctl: öppna, stoppa, sessioner och sammanfattningar.

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
)

// openTarget öppnar en port (8080, :8080) eller en länk i webbläsaren.
func (c *Controller) openTarget(a Args) (any, error) {
	t := strings.TrimPrefix(a.S("target"), ":")
	if _, err := strconv.Atoi(t); err == nil {
		t = "http://localhost:" + t
	}
	return c.openURL(Args{"url": t})
}

// stopPort stoppar det som lyssnar på en port: en lokal server, eller containern bakom en Docker-port.
func (c *Controller) stopPort(a Args) (any, error) {
	port, err := strconv.Atoi(strings.TrimPrefix(a.S("port"), ":"))
	if err != nil {
		return nil, errors.New("ange en port, t.ex. 8080")
	}
	if !a.B("confirm", false) {
		return nil, fmt.Errorf("att stoppa :%d kräver bekräftelse", port)
	}
	snap := c.col.Snapshot()
	for _, s := range snap.Servers {
		if s.Port != port {
			continue
		}
		if s.Kind == "docker" && s.Container != "" {
			for _, ct := range snap.Docker.Containers {
				if ct.Name == s.Container {
					return c.docker(Args{"op": "stop", "id": ct.ID})
				}
			}
		}
		return c.killProcess(Args{"pid": float64(s.PID)})
	}
	return nil, fmt.Errorf("inget lyssnar på :%d", port)
}

type sessionInfo struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Pane    string `json:"pane"`
	Tmux    string `json:"tmux"`
	Cwd     string `json:"cwd"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Title   string `json:"title"`
	Last    string `json:"last"`
	Prompt  string `json:"prompt"`
	Reply   string `json:"reply"`
	Files   int    `json:"files"`
	Session string `json:"session"`
	Transcr string `json:"-"`
}

func (c *Controller) sessionInfos() []sessionInfo {
	snap := c.col.Snapshot()
	var out []sessionInfo
	for _, s := range state.ClaudeSessions() {
		si := sessionInfo{Name: s.Name, Status: s.Status, Tmux: s.Tmux, Cwd: s.Cwd, Session: s.SessionID}
		if i := strings.LastIndex(s.Tmux, "."); i > 0 {
			si.Pane = s.Tmux[i+1:]
		}
		files, _ := filepath.Glob(config.Home("~/.claude/projects/*/" + s.SessionID + ".jsonl"))
		if len(files) > 0 {
			si.Transcr = files[0]
			if p, err := parseSession(files[0]); err == nil {
				si.Title, si.Files = p.Title, len(p.Files)
				if !p.Last.IsZero() {
					si.Last = p.Last.Local().Format("2006-01-02 15:04")
				}
				if len(p.Prompts) > 0 {
					si.Prompt = clip(strings.ReplaceAll(p.Prompts[len(p.Prompts)-1], "\n", " "), 200)
				}
				si.Reply = clip(strings.ReplaceAll(p.Reply, "\n", " "), 300)
				if r, wt := repoForSession(snap, p); r != nil {
					si.Repo = r.Name
					for _, w := range r.Worktrees {
						if w.Path == wt {
							si.Branch = w.Branch
						}
					}
				}
			}
		}
		out = append(out, si)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Last > out[j].Last })
	return out
}

func (c *Controller) sessionsAction(Args) (any, error) { return c.sessionInfos(), nil }

// summarize: en sessions sammanfattning (pane-id, sessionsnamn) eller en rad per session ("all").
func (c *Controller) summarize(a Args) (any, error) {
	target := a.S("target")
	infos := c.sessionInfos()
	if target == "" || target == "all" {
		var b strings.Builder
		for _, s := range infos {
			loc := s.Tmux
			if loc == "" {
				loc = "utanför tmux"
			}
			fmt.Fprintf(&b, "- %s (%s, %s) %s", s.Name, s.Status, loc, s.Title)
			if s.Repo != "" {
				fmt.Fprintf(&b, " · %s@%s", s.Repo, s.Branch)
			}
			if s.Last != "" {
				fmt.Fprintf(&b, " · senast %s", s.Last)
			}
			if s.Prompt != "" {
				fmt.Fprintf(&b, "\n  senaste önskemål: %s", s.Prompt)
			}
			if s.Reply != "" {
				fmt.Fprintf(&b, "\n  senaste svar: %s", s.Reply)
			}
			b.WriteString("\n")
		}
		return map[string]string{"text": b.String()}, nil
	}
	for _, s := range infos {
		if s.Name == target || s.Pane == target || s.Session == target {
			if s.Transcr == "" {
				return nil, fmt.Errorf("hittar ingen logg för %s", target)
			}
			p, err := parseSession(s.Transcr)
			if err != nil {
				return nil, err
			}
			wt := ""
			snap := c.col.Snapshot()
			if _, w := repoForSession(snap, p); w != "" {
				wt = w
			}
			return map[string]string{"text": fmt.Sprintf("Status: %s · %s\n\n", s.Status, s.Tmux) + sessionMarkdown(p, s.Repo, wt, time.Now())}, nil
		}
	}
	return nil, fmt.Errorf("ingen Claude-session %q (se devctl sessions)", target)
}

// AutoReloadOrchestrator laddar om orkestratorn när dess instruktioner har ändrats (t.ex. efter en uppdatering
// av devctl), men bara när den är ledig.
func (c *Controller) AutoReloadOrchestrator() {
	go func() {
		time.Sleep(45 * time.Second)
		for i := 0; i < 20; i++ {
			o := c.col.Snapshot().Orchestrator
			if o == nil {
				return
			}
			cached, _ := readCache("orchestrator-global.md")
			if cached == c.Brief() {
				return
			}
			if s := state.SessionInPane(o.Pane); s != nil && s.Status == "idle" {
				_, _ = c.orchestratorReload(nil)
				return
			}
			time.Sleep(3 * time.Minute)
		}
	}()
}
