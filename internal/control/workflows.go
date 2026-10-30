package control

// Lokala workflows (flera actions i följd) och GitHub Actions (köra om, starta för hand).

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

type stepRun struct {
	Label  string `json:"label"`
	Run    string `json:"run"`
	Dir    string `json:"dir"`
	Wait   string `json:"wait"`
	Status string `json:"status"` // väntar, kör, klar, misslyckades, hoppad
	Pane   string `json:"pane,omitempty"`
	Exit   *int   `json:"exit,omitempty"`
	Note   string `json:"note,omitempty"`
}

type workflowRun struct {
	ID       string     `json:"id"`
	Repo     string     `json:"repo"`
	Branch   string     `json:"branch"`
	Workflow string     `json:"workflow"`
	Status   string     `json:"status"` // kör, klar, misslyckades, avbruten
	Started  string     `json:"started"`
	Steps    []*stepRun `json:"steps"`
	cancel   chan struct{}
}

var (
	runsMu sync.Mutex
	runs   = map[string]*workflowRun{}
)

func snapshotRun(r *workflowRun) workflowRun {
	runsMu.Lock()
	defer runsMu.Unlock()
	cp := *r
	cp.Steps = make([]*stepRun, len(r.Steps))
	for i, s := range r.Steps {
		x := *s
		cp.Steps[i] = &x
	}
	return cp
}

var portRe = regexp.MustCompile(`^port\s*:?\s*(\d+)$`)

// runWorkflow startar ett lokalt workflow i bakgrunden och returnerar körningen.
func (c *Controller) runWorkflow(r *state.Repo, wt *state.Worktree, wf state.Workflow, actions []state.Action) (*workflowRun, error) {
	run := &workflowRun{ID: fmt.Sprintf("wf-%d", time.Now().UnixNano()), Repo: r.Name, Branch: wt.Branch, Workflow: wf.Label,
		Status: "kör", Started: time.Now().Format("2006-01-02 15:04:05"), cancel: make(chan struct{})}
	for _, st := range wf.Steps {
		s := &stepRun{Label: st.Label, Run: st.Run, Dir: st.Dir, Wait: strings.TrimSpace(st.Wait), Status: "väntar"}
		if st.Action != "" {
			var found *state.Action
			for i := range actions {
				if strings.EqualFold(actions[i].Label, st.Action) || actions[i].ID == st.Action ||
					strings.EqualFold(strings.SplitN(actions[i].Label, " · ", 2)[0], st.Action) && actions[i].Dir == st.Dir {
					found = &actions[i]
					break
				}
			}
			if found == nil {
				return nil, fmt.Errorf("steget %q: %s har ingen action med det namnet (se Kör-menyn)", st.Action, r.Name)
			}
			s.Run, s.Dir = found.Run, found.Dir
			if s.Label == "" {
				s.Label = found.Label
			}
		}
		if s.Run == "" {
			return nil, fmt.Errorf("ett steg i %q saknar action eller run", wf.Label)
		}
		if s.Label == "" {
			s.Label = s.Run
		}
		if s.Wait == "" {
			s.Wait = "exit"
		}
		run.Steps = append(run.Steps, s)
	}
	runsMu.Lock()
	runs[run.ID] = run
	runsMu.Unlock()

	go func() {
		set := func(s *stepRun, status, note string) {
			runsMu.Lock()
			s.Status, s.Note = status, note
			runsMu.Unlock()
		}
		finish := func(status string) {
			runsMu.Lock()
			run.Status = status
			for _, s := range run.Steps {
				if s.Status == "väntar" {
					s.Status = "hoppad"
				}
			}
			runsMu.Unlock()
		}
		parent := ""
		for i, s := range run.Steps {
			select {
			case <-run.cancel:
				finish("avbruten")
				return
			default:
			}
			exitFile := filepath.Join(config.CacheDir(), fmt.Sprintf("%s-%d.exit", run.ID, i))
			wid, pane, err := newWindowIn(c.cfg.Session, fmt.Sprintf("%s/%s: %s", r.Name, wt.Branch, s.Label), filepath.Join(wt.Path, s.Dir))
			if err != nil {
				set(s, "misslyckades", err.Error())
				finish("misslyckades")
				return
			}
			_, _ = tmux("set-option", "-w", "-t", wid, "automatic-rename", "off")
			_, _ = tmux("set-option", "-w", "-t", wid, "@kind", "workflow")
			if parent == "" {
				parent = wid
			} else {
				_ = setParent(wid, parent)
			}
			runsMu.Lock()
			s.Pane, s.Status = pane, "kör"
			runsMu.Unlock()
			// Underskal: ett "exit" i kommandot stänger inte fönstrets skal, så slutkoden sparas alltid
			line := `( ` + s.Run + ` ); code=$?; echo $code > ` + shellQuote(exitFile) + `; echo; echo "── klart (exit $code) ──"`
			if err := runIn(pane, line); err != nil {
				set(s, "misslyckades", err.Error())
				finish("misslyckades")
				return
			}
			ok, note := c.waitStep(run, s, exitFile)
			_ = os.Remove(exitFile)
			if !ok {
				set(s, "misslyckades", note)
				finish("misslyckades")
				return
			}
			runsMu.Lock()
			if s.Status == "kör" {
				s.Status = "klar"
			}
			s.Note = note
			runsMu.Unlock()
		}
		finish("klar")
	}()
	return run, nil
}

// waitStep väntar enligt stegets wait. Returnerar om workflowet ska fortsätta.
func (c *Controller) waitStep(run *workflowRun, s *stepRun, exitFile string) (bool, string) {
	deadline := time.Now().Add(30 * time.Minute)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	switch {
	case s.Wait == "none":
		return true, "körs vidare i sitt fönster"
	case strings.HasSuffix(s.Wait, "s") && !strings.HasPrefix(s.Wait, "port"):
		d, err := time.ParseDuration(s.Wait)
		if err != nil {
			return false, "ogiltig wait: " + s.Wait
		}
		select {
		case <-time.After(d):
			return true, "körs vidare i sitt fönster"
		case <-run.cancel:
			return false, "avbrutet"
		}
	case portRe.MatchString(s.Wait):
		port, _ := strconv.Atoi(portRe.FindStringSubmatch(s.Wait)[1])
		deadline = time.Now().Add(5 * time.Minute)
		for time.Now().Before(deadline) {
			if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond); err == nil {
				conn.Close()
				return true, fmt.Sprintf("svarar på :%d", port)
			}
			if b, err := os.ReadFile(exitFile); err == nil {
				return false, "avslutades innan porten svarade (exit " + strings.TrimSpace(string(b)) + ")"
			}
			select {
			case <-tick.C:
			case <-run.cancel:
				return false, "avbrutet"
			}
		}
		return false, fmt.Sprintf(":%d svarade inte inom 5 minuter", port)
	case s.Wait != "exit":
		return false, "okänd wait: " + s.Wait + " (exit, none, port 8080 eller t.ex. 10s)"
	}
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(exitFile); err == nil {
			code, _ := strconv.Atoi(strings.TrimSpace(string(b)))
			runsMu.Lock()
			s.Exit = &code
			runsMu.Unlock()
			if code != 0 {
				return false, fmt.Sprintf("exit %d", code)
			}
			return true, ""
		}
		select {
		case <-tick.C:
		case <-run.cancel:
			return false, "avbrutet"
		}
	}
	return false, "tog längre än 30 minuter"
}

func (c *Controller) workflowStatus(a Args) (any, error) {
	if id := a.S("id"); id != "" {
		runsMu.Lock()
		r := runs[id]
		runsMu.Unlock()
		if r == nil {
			return nil, errors.New("okänd körning")
		}
		return snapshotRun(r), nil
	}
	runsMu.Lock()
	list := make([]*workflowRun, 0, len(runs))
	for _, r := range runs {
		list = append(list, r)
	}
	runsMu.Unlock()
	sort.Slice(list, func(i, j int) bool { return list[i].ID > list[j].ID })
	out := []workflowRun{}
	for _, r := range list {
		out = append(out, snapshotRun(r))
	}
	return out, nil
}

func (c *Controller) workflowCancel(a Args) (any, error) {
	runsMu.Lock()
	r := runs[a.S("id")]
	runsMu.Unlock()
	if r == nil {
		return nil, errors.New("okänd körning")
	}
	select {
	case <-r.cancel:
	default:
		close(r.cancel)
	}
	return nil, nil
}

// ---------- GitHub Actions ----------

var refRe = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

func (c *Controller) ciRerun(a Args) (any, error) {
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	if r.GitHub == "" {
		return nil, errors.New("repot har ingen GitHub-remote")
	}
	id := a.I("run", 0)
	if id <= 0 {
		return nil, errors.New("ogiltig körning")
	}
	args := []string{"run", "rerun", fmt.Sprint(id), "-R", r.GitHub}
	if a.B("failed", true) {
		args = append(args, "--failed")
	}
	_, err = sysx.Run(30*time.Second, "gh", args...)
	c.col.Refresh(true)
	return nil, err
}

// ciDispatch startar en workflow_dispatch-workflow på en branch. Kräver confirm.
func (c *Controller) ciDispatch(a Args) (any, error) {
	r, err := FindRepo(c.col.Snapshot(), a.S("repo"))
	if err != nil {
		return nil, err
	}
	var wf *state.DispatchWorkflow
	for i := range r.Dispatch {
		if r.Dispatch[i].File == a.S("file") {
			wf = &r.Dispatch[i]
		}
	}
	if wf == nil {
		return nil, fmt.Errorf("%s har ingen workflow %q som kan startas för hand", r.Name, a.S("file"))
	}
	ref := a.S("ref")
	if !refRe.MatchString(ref) {
		return nil, errors.New("ogiltig branch")
	}
	if !a.B("confirm", false) {
		return nil, fmt.Errorf("%q kräver bekräftelse", wf.Name)
	}
	args := []string{"workflow", "run", wf.File, "-R", r.GitHub, "--ref", ref}
	inputs, _ := a["inputs"].(map[string]any)
	for _, in := range wf.Inputs {
		v, ok := inputs[in.Name]
		if !ok {
			continue
		}
		s := strings.TrimSpace(fmt.Sprint(v))
		if s == "" {
			if in.Required {
				return nil, fmt.Errorf("%s krävs", in.Name)
			}
			continue
		}
		args = append(args, "-f", in.Name+"="+s)
	}
	out, err := sysx.Combined(30*time.Second, "gh", args...)
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(out))
	}
	c.col.Refresh(true)
	return map[string]string{"output": strings.TrimSpace(out), "actions": "https://github.com/" + r.GitHub + "/actions/workflows/" + wf.File}, nil
}
