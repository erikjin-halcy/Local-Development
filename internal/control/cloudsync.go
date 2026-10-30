package control

// Sammanfattning till molnet: devctl skriver läget (sessioner, ändringar per branch, PR/CI, öppna fynd,
// parkerat, vad som körs) till ett Firestore-dokument, så att det går att se på mobilen även när Macen är av.
// Skrivs bara när innehållet ändrats, och minst en gång i timmen så att det syns att Macen är på.

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

var (
	cloudMu       sync.Mutex
	cloudLastHash string
	cloudLastPush time.Time
	cloudLastErr  string
)

func (c *Controller) cloudSummary() map[string]any {
	snap := c.col.Snapshot()
	host, _ := os.Hostname()
	out := map[string]any{"host": strings.Split(host, ".")[0]}

	if o := snap.Orchestrator; o != nil {
		st := ""
		if s := state.SessionInPane(o.Pane); s != nil {
			st = s.Status
		}
		out["orchestrator"] = map[string]any{"status": st}
	}

	var sessions []any
	for _, s := range c.sessionInfos() {
		if s.Last == "" || time.Since(parseLocal(s.Last)) > 24*time.Hour {
			continue // bara sessioner som varit aktiva det senaste dygnet
		}
		sessions = append(sessions, map[string]any{"name": s.Name, "status": s.Status, "repo": s.Repo, "branch": s.Branch,
			"title": s.Title, "last": s.Last, "prompt": clip(s.Prompt, 160), "reply": clip(s.Reply, 240)})
	}
	out["sessions"] = sessions

	servers := map[string][]string{}
	for _, sv := range snap.Servers {
		if sv.App || sv.Worktree == nil {
			continue
		}
		k := sv.Worktree.Repo + "@" + sv.Worktree.Branch
		if len(servers[k]) < 6 {
			servers[k] = append(servers[k], fmt.Sprintf(":%d %s", sv.Port, sv.Kind))
		}
	}
	var repos []any
	for _, r := range snap.Repos {
		if !r.Active || r.Archived {
			continue
		}
		var branches []any
		for _, wt := range r.Worktrees {
			if wt.Missing || wt.Branch == "" {
				continue
			}
			b := map[string]any{"branch": wt.Branch, "changes": wt.Changes, "ahead": wt.Ahead, "behind": wt.Behind,
				"last": wt.Last.Subject, "panes": len(wt.Panes), "containers": len(wt.Containers),
				"servers": servers[r.Name+"@"+wt.Branch]}
			if wt.PR != nil {
				b["pr"] = fmt.Sprintf("#%d %s", wt.PR.Number, wt.PR.Title)
				b["prUrl"] = wt.PR.URL
			}
			if wt.CI != nil {
				b["ci"] = wt.CI.State
			}
			branches = append(branches, b)
		}
		repos = append(repos, map[string]any{"name": r.Name, "github": r.GitHub, "prs": len(r.PRs), "parked": r.Parked, "branches": branches})
	}
	out["repos"] = repos

	var findings []any
	agents, _ := config.Agents()
	for _, a := range agents {
		for _, f := range loadAgentState(a.Name).Findings {
			if f.Status == "öppen" {
				findings = append(findings, map[string]any{"agent": a.Icon + " " + a.Label, "id": f.ID, "severity": f.Severity,
					"repo": f.Repo, "branch": f.Branch, "file": f.File, "rule": f.Rule, "finding": clip(f.Finding, 220)})
			}
		}
	}
	out["findings"] = findings

	var parked []any
	for _, t := range config.Todos(c.cfg) {
		if t.Status != "klar" {
			parked = append(parked, map[string]any{"repo": t.Repo, "text": clip(t.Title, 200), "date": t.Created, "status": t.Status})
		}
	}
	out["parked"] = parked

	running := 0
	for _, ct := range snap.Docker.Containers {
		if ct.State == "running" {
			running++
		}
	}
	devServers := 0
	for _, sv := range snap.Servers {
		if !sv.App {
			devServers++
		}
	}
	out["counts"] = map[string]any{"servers": devServers, "containers": running, "sessions": len(sessions)}
	return out
}

func parseLocal(s string) time.Time {
	t, _ := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	return t
}

// firestoreValue gör om ett Go-värde till Firestores REST-format.
func firestoreValue(v any) map[string]any {
	switch x := v.(type) {
	case nil:
		return map[string]any{"nullValue": nil}
	case bool:
		return map[string]any{"booleanValue": x}
	case int:
		return map[string]any{"integerValue": fmt.Sprint(x)}
	case float64:
		return map[string]any{"doubleValue": x}
	case string:
		return map[string]any{"stringValue": x}
	case time.Time:
		return map[string]any{"timestampValue": x.UTC().Format(time.RFC3339)}
	case []string:
		vals := []any{}
		for _, s := range x {
			vals = append(vals, firestoreValue(s))
		}
		return map[string]any{"arrayValue": map[string]any{"values": vals}}
	case []any:
		vals := []any{}
		for _, s := range x {
			vals = append(vals, firestoreValue(s))
		}
		return map[string]any{"arrayValue": map[string]any{"values": vals}}
	case map[string]any:
		f := map[string]any{}
		for k, s := range x {
			f[k] = firestoreValue(s)
		}
		return map[string]any{"mapValue": map[string]any{"fields": f}}
	}
	return map[string]any{"stringValue": fmt.Sprint(v)}
}

// PushCloud skriver sammanfattningen om den ändrats (eller force, eller efter en timme).
func (c *Controller) PushCloud(force bool) error {
	cl := c.cfg.Cloud
	if cl.Project == "" {
		return errors.New("cloud.project är inte satt i konfigurationen")
	}
	cloudMu.Lock()
	defer cloudMu.Unlock()
	sum := c.cloudSummary()
	b, _ := json.Marshal(sum)
	h := sha1.Sum(b)
	hash := fmt.Sprintf("%x", h[:8])
	if !force && hash == cloudLastHash && time.Since(cloudLastPush) < time.Hour {
		return nil
	}
	sum["updatedAt"] = time.Now()
	fields := map[string]any{}
	keys := make([]string, 0, len(sum))
	for k := range sum {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fields[k] = firestoreValue(sum[k])
	}
	body, _ := json.Marshal(map[string]any{"fields": fields})
	token, err := sysx.Run(20*time.Second, "gcloud", "auth", "print-access-token")
	if err != nil {
		cloudLastErr = "gcloud: " + err.Error()
		return errors.New(cloudLastErr)
	}
	url := fmt.Sprintf("https://firestore.googleapis.com/v1/projects/%s/databases/(default)/documents/%s", cl.Project, cl.Doc)
	req, _ := http.NewRequest("PATCH", url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	req.Header.Set("x-goog-user-project", cl.Project)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		cloudLastErr = err.Error()
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		msg, _ := io.ReadAll(resp.Body)
		cloudLastErr = fmt.Sprintf("Firestore %d: %s", resp.StatusCode, clip(string(msg), 200))
		return errors.New(cloudLastErr)
	}
	cloudLastHash, cloudLastPush, cloudLastErr = hash, time.Now(), ""
	return nil
}

// StartCloudSync skickar sammanfattningen med jämna mellanrum medan devctl körs.
func (c *Controller) StartCloudSync() {
	if c.cfg.Cloud.Project == "" {
		return
	}
	every := time.Duration(max(c.cfg.Cloud.EveryMinutes, 5)) * time.Minute
	go func() {
		time.Sleep(60 * time.Second) // låt läget och repos läsas in först
		for {
			_ = c.PushCloud(false)
			time.Sleep(every)
		}
	}()
}

func (c *Controller) cloudPush(Args) (any, error) {
	if err := c.PushCloud(true); err != nil {
		return nil, err
	}
	return map[string]string{"pushed": cloudLastPush.Format("15:04")}, nil
}

func (c *Controller) cloudStatus(Args) (any, error) {
	cloudMu.Lock()
	defer cloudMu.Unlock()
	last := ""
	if !cloudLastPush.IsZero() {
		last = cloudLastPush.Format("2006-01-02 15:04")
	}
	return map[string]string{"project": c.cfg.Cloud.Project, "last": last, "error": cloudLastErr}, nil
}
