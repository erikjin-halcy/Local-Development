package control

// Orkestratorns samtal läst ur dess Claude Code-logg, med avsändare: du (i devctl eller terminalen),
// en annan Claude-session (Claudes meddelanden mellan sessioner eller devctl pane send) eller Claude Code självt.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
)

// ---------- logg över det devctl skickar till panes ----------

type sentEntry struct {
	Time  int64  `json:"t"`
	Pane  string `json:"pane"`
	Text  string `json:"text"`
	Via   string `json:"via"` // app, cli
	By    string `json:"by"`  // namnet på Claude-sessionen som körde devctl, annars tomt
	Where string `json:"where,omitempty"`
}

var sentMu sync.Mutex

func sentPath() string { return filepath.Join(config.CacheDir(), "sent.jsonl") }

func logSent(e sentEntry) {
	sentMu.Lock()
	defer sentMu.Unlock()
	e.Time = time.Now().Unix()
	b, _ := json.Marshal(e)
	f, err := os.OpenFile(sentPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func readSent(pane string) []sentEntry {
	sentMu.Lock()
	defer sentMu.Unlock()
	f, err := os.Open(sentPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []sentEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	for sc.Scan() {
		var e sentEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil && e.Pane == pane {
			out = append(out, e)
		}
	}
	return out
}

// ---------- samtalet ----------

type chatItem struct {
	Role  string `json:"role"` // user, bot, tool, system
	From  string `json:"from"` // du, session, system
	Who   string `json:"who"`  // visningsnamn för avsändaren
	Where string `json:"where,omitempty"`
	Text  string `json:"text"`
	Time  string `json:"time"`
}

var crossRe = regexp.MustCompile(`(?s)<cross-session-message from="([^"]*)"(?: from-name="([^"]*)")?[^>]*>\s*(.*?)\s*(?:</cross-session-message>|\n\nThis came from another Claude session|$)`)
var cmdRe = regexp.MustCompile(`<command-name>([^<]+)</command-name>`)
var pastedRe = regexp.MustCompile(`(?s)^<pasted_content[^>]*>\s*(.*?)\s*</pasted_content(?:\s+id="[^"]*")?>$`)

func where(s *state.ClaudeSession) string {
	if s == nil {
		return ""
	}
	parts := []string{}
	if s.Tmux != "" {
		t := s.Tmux
		if i := strings.Index(t, ":@"); i > 0 {
			t = t[:i]
		}
		parts = append(parts, "tmux "+t)
	}
	if s.Cwd != "" {
		parts = append(parts, "~"+strings.TrimPrefix(s.Cwd, config.Home("~")))
	}
	return strings.Join(parts, " · ")
}

func (c *Controller) orchHistory(a Args) (any, error) {
	o := c.col.Snapshot().Orchestrator
	if o == nil {
		return nil, fmt.Errorf("orkestratorn körs inte")
	}
	sess := state.SessionInPane(o.Pane)
	if sess == nil {
		return map[string]any{"items": []chatItem{}, "status": "", "note": "hittar inte orkestratorns session"}, nil
	}
	files, _ := filepath.Glob(config.Home("~/.claude/projects/*/" + sess.SessionID + ".jsonl"))
	if len(files) == 0 {
		return map[string]any{"items": []chatItem{}, "status": sess.Status}, nil
	}
	sent := readSent(o.Pane)
	used := map[int]bool{}
	var items []chatItem
	f, err := os.Open(files[0])
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rd := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 0 {
			var r struct {
				Type         string `json:"type"`
				Timestamp    string `json:"timestamp"`
				IsMeta       bool   `json:"isMeta"`
				PromptSource string `json:"promptSource"`
				Message      struct {
					Content json.RawMessage `json:"content"`
				} `json:"message"`
			}
			if (strings.Contains(string(line[:min(len(line), 300)]), `"type":"user"`) || strings.Contains(string(line), `"type":"user"`) || strings.Contains(string(line), `"type":"assistant"`)) &&
				json.Unmarshal(line, &r) == nil && (r.Type == "user" || r.Type == "assistant") {
				ts, _ := time.Parse(time.RFC3339Nano, r.Timestamp)
				at := ts.Local().Format("15:04")
				if r.Type == "user" {
					var text string
					if json.Unmarshal(r.Message.Content, &text) != nil {
						goto next
					}
					// Meddelanden från andra sessioner är märkta som metadata; annan metadata hoppas över
					if r.IsMeta && !strings.Contains(text, "<cross-session-message") {
						goto next
					}
					text = strings.TrimSpace(text)
					// Långa inskrivna meddelanden sparas som inklistrad text; packa upp dem
					if m := pastedRe.FindStringSubmatch(text); m != nil {
						text = strings.TrimSpace(m[1])
					}
					switch {
					case strings.HasPrefix(text, "<local-command-caveat>") || strings.HasPrefix(text, "<local-command-stdout>"):
					case cmdRe.MatchString(text):
						items = append(items, chatItem{Role: "system", From: "system", Who: "Kommando", Text: cmdRe.FindStringSubmatch(text)[1], Time: at})
					case crossRe.MatchString(text):
						m := crossRe.FindStringSubmatch(text)
						s := state.SessionBySocket(m[1])
						name := m[2]
						if name == "" && s != nil {
							name = s.Name
						}
						if name == "" {
							name = "okänd session"
						}
						items = append(items, chatItem{Role: "user", From: "session", Who: name, Where: where(s), Text: m[3], Time: at})
					case strings.HasPrefix(text, "<") || strings.HasPrefix(text, "[Cross-session"):
						items = append(items, chatItem{Role: "system", From: "system", Who: "Claude Code", Text: clip(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(text, ""), 300), Time: at})
					default:
						it := chatItem{Role: "user", From: "du", Who: "Du", Where: "i terminalen", Text: text, Time: at}
						// Skickat via devctl? Matcha mot loggen över det devctl skickat till panen
						for i := len(sent) - 1; i >= 0; i-- {
							e := sent[i]
							if used[i] || strings.TrimSpace(e.Text) != text || ts.Unix()-e.Time > 300 || e.Time-ts.Unix() > 60 {
								continue
							}
							used[i] = true
							if e.Via == "agent" {
								it.From, it.Who, it.Where = "agent", e.By, "agent i devctl"
							} else if e.By != "" {
								it.From, it.Who, it.Where = "session", e.By, strings.TrimSpace("via devctl · "+e.Where)
							} else if e.Via == "app" {
								it.Where = "i devctl"
							} else {
								it.Where = "via devctl-kommandot"
							}
							break
						}
						items = append(items, it)
					}
				} else {
					var blocks []struct {
						Type  string         `json:"type"`
						Text  string         `json:"text"`
						Name  string         `json:"name"`
						Input map[string]any `json:"input"`
					}
					if json.Unmarshal(r.Message.Content, &blocks) == nil {
						for _, b := range blocks {
							switch {
							case b.Type == "text" && strings.TrimSpace(b.Text) != "":
								items = append(items, chatItem{Role: "bot", From: "orkestrator", Who: "Orkestratorn", Text: b.Text, Time: at})
							case b.Type == "tool_use":
								d := ""
								for _, k := range []string{"description", "command", "file_path", "to"} {
									if v, ok := b.Input[k].(string); ok && v != "" {
										d = v
										break
									}
								}
								items = append(items, chatItem{Role: "tool", From: "orkestrator", Who: b.Name, Text: clip(strings.ReplaceAll(d, "\n", " "), 160), Time: at})
							}
						}
					}
				}
			}
		}
	next:
		if err != nil {
			break
		}
	}
	if len(items) > 300 {
		items = items[len(items)-300:]
	}
	if items == nil {
		items = []chatItem{}
	}
	return map[string]any{"items": items, "status": sess.Status, "name": sess.Name}, nil
}
