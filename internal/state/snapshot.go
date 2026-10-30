package state

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
)

type Orchestrator struct {
	Session string `json:"session"`
	Window  string `json:"window"`
	Pane    string `json:"pane"`
	Command string `json:"command"`
}

type Snapshot struct {
	Time         time.Time         `json:"time"`
	Hostname     string            `json:"hostname"`
	Repos        []Repo            `json:"repos"`
	Sessions     []Session         `json:"sessions"`
	Servers      []Server          `json:"servers"`
	Docker       Docker            `json:"docker"`
	Orchestrator *Orchestrator     `json:"orchestrator"`
	Vault        string            `json:"vault"`
	Errors       map[string]string `json:"errors"`
}

// Collector håller en färsk ögonblicksbild. tmux, servrar och Docker läses varannan sekund,
// git var 10:e sekund, PR:er från GitHub varannan minut och nya repos letas upp varannan minut.
type Collector struct {
	cfg config.Config

	mu   sync.RWMutex
	snap Snapshot

	gitMu    sync.Mutex
	repos    map[string]*Repo  // gemensam .git-katalog -> repo
	dirRepo  map[string]string // katalog -> gemensam .git-katalog ("" = inget repo)
	prs      map[string][]PR   // owner/repo -> öppna PR:er
	errors   map[string]string
	fastKick chan struct{}
	gitKick  chan struct{}
}

func NewCollector(cfg config.Config) *Collector {
	return &Collector{
		cfg: cfg, repos: map[string]*Repo{}, dirRepo: map[string]string{}, prs: map[string][]PR{},
		errors: map[string]string{}, fastKick: make(chan struct{}, 1), gitKick: make(chan struct{}, 1),
	}
}

func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snap
}

// Refresh begär en ny läsning direkt; withGit läser även om alla repos.
func (c *Collector) Refresh(withGit bool) {
	if withGit {
		select {
		case c.gitKick <- struct{}{}:
		default:
		}
		return
	}
	select {
	case c.fastKick <- struct{}{}:
	default:
	}
}

// RefreshNow läser om allt synkront (används av CLI-kommandon som behöver aktuellt läge).
func (c *Collector) RefreshNow() Snapshot {
	c.refreshGit()
	c.refreshFast()
	return c.Snapshot()
}

func (c *Collector) Start() {
	c.discover()
	c.refreshGit()
	c.refreshFast()
	go c.loop(2*time.Second, c.fastKick, c.refreshFast)
	go c.loop(10*time.Second, c.gitKick, func() { c.refreshGit(); c.refreshFast() })
	go c.loop(2*time.Minute, nil, c.discover)
	go c.loop(2*time.Minute, nil, func() { c.refreshPRs(); c.refreshFast() })
	go func() { time.Sleep(2 * time.Second); c.refreshPRs(); c.refreshFast() }()
}

func (c *Collector) loop(every time.Duration, kick chan struct{}, fn func()) {
	t := time.NewTicker(every)
	for {
		select {
		case <-t.C:
		case <-kick:
		}
		fn()
	}
}

func (c *Collector) setErr(key string, err error) {
	c.gitMu.Lock()
	defer c.gitMu.Unlock()
	if err != nil {
		c.errors[key] = err.Error()
	} else {
		delete(c.errors, key)
	}
}

// repoFor returnerar den gemensamma .git-katalogen för dir och kommer ihåg svaret.
func (c *Collector) repoFor(dir string) string {
	c.gitMu.Lock()
	common, ok := c.dirRepo[dir]
	c.gitMu.Unlock()
	if ok {
		return common
	}
	common, _ = commonDir(dir)
	c.gitMu.Lock()
	c.dirRepo[dir] = common
	c.gitMu.Unlock()
	return common
}

func (c *Collector) discover() {
	for _, dir := range discover(c.cfg.Roots, c.cfg.ScanDepth) {
		if common := c.repoFor(dir); common != "" {
			c.gitMu.Lock()
			if c.repos[common] == nil {
				c.repos[common] = &Repo{Name: filepath.Base(dir), Path: dir, CommonDir: common}
			}
			c.gitMu.Unlock()
		}
	}
}

// AddDir ser till att repot som dir ligger i är känt (t.ex. efter att en worktree skapats).
func (c *Collector) AddDir(dir string) {
	c.gitMu.Lock()
	delete(c.dirRepo, dir)
	c.gitMu.Unlock()
	if common := c.repoFor(dir); common != "" {
		c.gitMu.Lock()
		if c.repos[common] == nil {
			c.repos[common] = &Repo{Path: dir, CommonDir: common}
		}
		c.gitMu.Unlock()
	}
}

func (c *Collector) refreshGit() {
	c.gitMu.Lock()
	commons := make([]string, 0, len(c.repos))
	for k := range c.repos {
		commons = append(commons, k)
	}
	c.gitMu.Unlock()

	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	var mu sync.Mutex
	fresh := map[string]*Repo{}
	for _, common := range commons {
		if _, err := os.Stat(common); err != nil {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(common string) {
			defer func() { <-sem; wg.Done() }()
			r := loadRepo(common)
			mu.Lock()
			fresh[common] = r
			mu.Unlock()
		}(common)
	}
	wg.Wait()
	c.gitMu.Lock()
	c.repos = fresh
	c.gitMu.Unlock()
}

func (c *Collector) refreshPRs() {
	c.gitMu.Lock()
	var gh []string
	for _, r := range c.repos {
		if r.GitHub != "" {
			gh = append(gh, r.GitHub)
		}
	}
	c.gitMu.Unlock()
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for _, name := range gh {
		wg.Add(1)
		sem <- struct{}{}
		go func(name string) {
			defer func() { <-sem; wg.Done() }()
			prs, err := loadPRs(name)
			c.gitMu.Lock()
			if err == nil {
				c.prs[name] = prs
				delete(c.errors, "gh:"+name)
			}
			c.gitMu.Unlock()
		}(name)
	}
	wg.Wait()
}

type wtIndex struct {
	path string
	ref  Ref
	repo int
	wt   int
}

func (c *Collector) refreshFast() {
	sessions, terr := collectTmux()
	c.setErr("tmux", terr)

	// Okända kataloger i panes blir nya repos vid nästa git-läsning
	paneByPID := map[int]string{}
	for _, s := range sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				paneByPID[p.PID] = p.ID
			}
		}
	}
	servers := collectServers(paneByPID)
	docker := collectDocker()

	// Kopiera repos så att ögonblicksbilden kan ändras fritt
	c.gitMu.Lock()
	repos := make([]Repo, 0, len(c.repos))
	for _, r := range c.repos {
		if r.Worktrees == nil {
			continue
		}
		cp := *r
		cp.Worktrees = append([]Worktree{}, r.Worktrees...)
		cp.Branches = append([]Branch{}, r.Branches...)
		cp.PRs = append([]PR{}, c.prs[r.GitHub]...)
		repos = append(repos, cp)
	}
	errs := map[string]string{}
	for k, v := range c.errors {
		errs[k] = v
	}
	c.gitMu.Unlock()
	sort.Slice(repos, func(i, j int) bool { return strings.ToLower(repos[i].Name) < strings.ToLower(repos[j].Name) })

	var idx []wtIndex
	for ri := range repos {
		r := &repos[ri]
		prByHead := map[string]*PR{}
		for i := range r.PRs {
			prByHead[r.PRs[i].Head] = &r.PRs[i]
		}
		for wi := range r.Worktrees {
			wt := &r.Worktrees[wi]
			wt.Panes, wt.Servers, wt.Containers = []string{}, []int{}, []string{}
			wt.PR = prByHead[wt.Branch]
			wt.Note = ""
			if p := NotePath(c.cfg, r.Name, wt.Branch); p != "" {
				if _, err := os.Stat(p); err == nil {
					wt.Note = p
				}
			}
			idx = append(idx, wtIndex{path: wt.Path, ref: Ref{Repo: r.Name, Path: wt.Path, Branch: branchLabel(*wt)}, repo: ri, wt: wi})
		}
		for bi := range r.Branches {
			r.Branches[bi].PR = prByHead[r.Branches[bi].Name]
		}
	}
	sort.Slice(idx, func(i, j int) bool { return len(idx[i].path) > len(idx[j].path) })
	match := func(dir string) *wtIndex {
		for i := range idx {
			if dir == idx[i].path || strings.HasPrefix(dir, idx[i].path+"/") {
				return &idx[i]
			}
		}
		return nil
	}
	var unknown []string

	var orch *Orchestrator
	for si := range sessions {
		for wi := range sessions[si].Windows {
			w := &sessions[si].Windows[wi]
			for pi := range w.Panes {
				p := &w.Panes[pi]
				if m := match(p.Path); m != nil {
					ref := m.ref
					p.Worktree = &ref
					wt := &repos[m.repo].Worktrees[m.wt]
					wt.Panes = append(wt.Panes, p.ID)
				} else if p.Path != "" {
					unknown = append(unknown, p.Path)
				}
				if w.Kind == "global-orchestrator" && (orch == nil || p.Role == "orchestrator") {
					orch = &Orchestrator{Session: sessions[si].Name, Window: w.ID, Pane: p.ID, Command: p.Command}
				}
			}
		}
	}

	paneIdx := map[string]*Pane{}
	for si := range sessions {
		for wi := range sessions[si].Windows {
			for pi := range sessions[si].Windows[wi].Panes {
				p := &sessions[si].Windows[wi].Panes[pi]
				paneIdx[p.ID] = p
			}
		}
	}
	for ci := range docker.Containers {
		ct := &docker.Containers[ci]
		if ct.Dir == "" {
			continue
		}
		if m := match(ct.Dir); m != nil {
			ref := m.ref
			ct.Worktree = &ref
			wt := &repos[m.repo].Worktrees[m.wt]
			wt.Containers = append(wt.Containers, ct.Name)
		}
	}
	for si := range servers {
		s := &servers[si]
		if m := match(s.Cwd); m != nil {
			ref := m.ref
			s.Worktree = &ref
		} else if s.Cwd != "" && s.Cwd != "/" && strings.HasPrefix(s.Cwd, "/Users/") {
			unknown = append(unknown, s.Cwd)
		}
		for ci := range docker.Containers {
			for _, hp := range docker.Containers[ci].HostPorts {
				if hp == s.Port {
					s.Container = docker.Containers[ci].Name
					if s.Worktree == nil {
						s.Worktree = docker.Containers[ci].Worktree
					}
				}
			}
		}
		classify(s)
		if s.Worktree != nil {
			if m := match(s.Worktree.Path); m != nil {
				wt := &repos[m.repo].Worktrees[m.wt]
				if !containsInt(wt.Servers, s.Port) {
					wt.Servers = append(wt.Servers, s.Port)
				}
			}
		}
		if p := paneIdx[s.Pane]; p != nil {
			p.Ports = append(p.Ports, s.Port)
		}
	}

	weekAgo := time.Now().Add(-3 * 24 * time.Hour).Unix()
	for ri := range repos {
		r := &repos[ri]
		r.Active = len(r.PRs) > 0 || r.LastUnix > weekAgo || len(r.Worktrees) > 1
		for _, wt := range r.Worktrees {
			if len(wt.Panes)+len(wt.Servers)+len(wt.Containers) > 0 || wt.Changes > 0 || wt.Last.Unix > weekAgo {
				r.Active = true
			}
		}
	}

	host, _ := os.Hostname()
	c.mu.Lock()
	c.snap = Snapshot{
		Time: time.Now(), Hostname: strings.Split(host, ".")[0], Repos: repos, Sessions: sessions,
		Servers: servers, Docker: docker, Orchestrator: orch, Vault: c.cfg.Obsidian.Vault, Errors: errs,
	}
	if c.snap.Sessions == nil {
		c.snap.Sessions = []Session{}
	}
	c.mu.Unlock()

	// Upptäck repos utanför rötterna (t.ex. en pane i ~/code/x)
	for _, dir := range unknown {
		if common := c.repoFor(dir); common != "" {
			c.gitMu.Lock()
			if c.repos[common] == nil {
				c.repos[common] = &Repo{Path: dir, CommonDir: common}
				c.Refresh(true)
			}
			c.gitMu.Unlock()
		}
	}
}

func branchLabel(wt Worktree) string {
	if wt.Branch != "" {
		return wt.Branch
	}
	return "(" + wt.Head + ")"
}

// NotePath är sökvägen till branchens anteckning i Obsidian-valvet ("" om inget valv är valt).
func NotePath(cfg config.Config, repo, branch string) string {
	if cfg.Obsidian.Vault == "" || branch == "" || strings.HasPrefix(branch, "(") {
		return ""
	}
	safe := strings.NewReplacer("/", " ∕ ", "\\", "-", ":", "-").Replace(branch)
	return filepath.Join(cfg.Obsidian.Vault, cfg.Obsidian.BranchDir, repo, safe+".md")
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
