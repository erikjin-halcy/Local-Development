package state

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

type Commit struct {
	Hash    string `json:"hash"`
	Subject string `json:"subject"`
	When    string `json:"when"`
	Author  string `json:"author"`
	Unix    int64  `json:"unix"`
}

type PR struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Head   string `json:"head"`
	State  string `json:"state"`
	URL    string `json:"url"`
	Draft  bool   `json:"draft"`
	Review string `json:"review"`
}

type Worktree struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Head     string `json:"head"`
	Main     bool   `json:"main"`
	Detached bool   `json:"detached"`
	Locked   bool   `json:"locked"`
	Missing  bool   `json:"missing"`
	Changes  int    `json:"changes"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Upstream string `json:"upstream"`
	Last     Commit `json:"last"`
	PR       *PR    `json:"pr,omitempty"`
	CI       *CI    `json:"ci,omitempty"`
	// Fylls i av snapshot: vad som körs i worktreen
	Panes      []string `json:"panes"`
	Servers    []int    `json:"servers"`
	Containers []string `json:"containers"`
	Note       string   `json:"note,omitempty"`
}

type Branch struct {
	Name     string `json:"name"`
	Upstream string `json:"upstream"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	Gone     bool   `json:"gone"`
	When     string `json:"when"`
	Unix     int64  `json:"unix"`
	Subject  string `json:"subject"`
	Worktree string `json:"worktree,omitempty"`
	PR       *PR    `json:"pr,omitempty"`
	CI       *CI    `json:"ci,omitempty"`
}

type Repo struct {
	Name      string     `json:"name"`
	Path      string     `json:"path"`
	CommonDir string     `json:"-"`
	Remote    string     `json:"remote"`
	GitHub    string     `json:"github"`
	Default   string     `json:"default"`
	Worktrees []Worktree `json:"worktrees"`
	Branches  []Branch   `json:"branches"`
	Actions   []Action   `json:"actions"`
	Workflows []Workflow `json:"workflows"`
	// GitHub Actions-workflows som går att starta för hand
	Dispatch []DispatchWorkflow `json:"dispatch"`
	PRs      []PR               `json:"prs"`
	Active   bool               `json:"active"`
	Archived bool               `json:"archived"`
	// Busy: något körs i repot (panes, servrar eller containrar)
	Busy     bool   `json:"busy"`
	LastUnix int64  `json:"lastUnix"`
	Error    string `json:"error,omitempty"`
}

var skipDirs = map[string]bool{
	"node_modules": true, "Library": true, "vendor": true, "dist": true, "build": true, ".venv": true,
	"venv": true, "__pycache__": true, ".next": true, "target": true, ".cache": true, ".Trash": true,
}

func git(dir string, args ...string) (string, error) {
	return sysx.RunIn(dir, 8*time.Second, "git", args...)
}

// commonDir returnerar repots gemensamma .git-katalog; samma för alla worktrees i ett repo.
func commonDir(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	return filepath.Clean(strings.TrimSpace(out)), nil
}

// TopLevel returnerar roten på worktreen som innehåller dir, eller "".
func TopLevel(dir string) string {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// discover letar efter git-repos under rötterna (till och med depth nivåer ner).
func discover(roots []string, depth int) []string {
	var found []string
	for _, root := range roots {
		root = filepath.Clean(root)
		base := strings.Count(root, string(os.PathSeparator))
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return fs.SkipDir
			}
			if !d.IsDir() {
				return nil
			}
			name := d.Name()
			if p != root && (skipDirs[name] || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".app")) {
				return fs.SkipDir
			}
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				found = append(found, p)
				return fs.SkipDir
			}
			if strings.Count(p, string(os.PathSeparator))-base >= depth {
				return fs.SkipDir
			}
			return nil
		})
	}
	return found
}

var ghRe = regexp.MustCompile(`github\.com[:/]([^/]+/[^/]+?)(\.git)?$`)

func loadRepo(common string) *Repo {
	main := common
	if filepath.Base(common) == ".git" {
		main = filepath.Dir(common)
	}
	r := &Repo{Name: filepath.Base(main), Path: main, CommonDir: common, Worktrees: []Worktree{}, Branches: []Branch{}, PRs: []PR{}}
	if out, err := git(main, "remote", "get-url", "origin"); err == nil {
		r.Remote = strings.TrimSpace(out)
		if m := ghRe.FindStringSubmatch(r.Remote); m != nil {
			r.GitHub = m[1]
		}
	}
	if out, err := git(main, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		r.Default = strings.TrimPrefix(strings.TrimSpace(out), "origin/")
	}

	out, err := git(main, "worktree", "list", "--porcelain")
	if err != nil {
		r.Error = err.Error()
		return r
	}
	for i, block := range strings.Split(strings.TrimSpace(out), "\n\n") {
		var wt Worktree
		for _, l := range strings.Split(block, "\n") {
			k, v, _ := strings.Cut(l, " ")
			switch k {
			case "worktree":
				wt.Path = v
			case "HEAD":
				if len(v) > 7 {
					v = v[:7]
				}
				wt.Head = v
			case "branch":
				wt.Branch = strings.TrimPrefix(v, "refs/heads/")
			case "detached":
				wt.Detached = true
			case "locked":
				wt.Locked = true
			case "prunable":
				wt.Missing = true
			case "bare":
				wt.Missing = true
			}
		}
		if wt.Path == "" {
			continue
		}
		wt.Main = i == 0
		wt.Panes, wt.Servers, wt.Containers = []string{}, []int{}, []string{}
		r.Worktrees = append(r.Worktrees, wt)
	}

	var wg sync.WaitGroup
	for i := range r.Worktrees {
		if r.Worktrees[i].Missing {
			continue
		}
		wg.Add(1)
		go func(wt *Worktree) {
			defer wg.Done()
			worktreeStatus(wt)
		}(&r.Worktrees[i])
	}
	wg.Wait()
	if r.Default == "" && len(r.Worktrees) > 0 {
		r.Default = r.Worktrees[0].Branch
	}

	const f = "%(refname:short)%1f%(upstream:short)%1f%(upstream:track,nobracket)%1f%(committerdate:relative)%1f%(committerdate:unix)%1f%(contents:subject)"
	if out, err := git(main, "for-each-ref", "--sort=-committerdate", "--count=40", "--format="+f, "refs/heads"); err == nil {
		inWt := map[string]string{}
		for _, wt := range r.Worktrees {
			if wt.Branch != "" {
				inWt[wt.Branch] = wt.Path
			}
		}
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			p := strings.Split(l, sep)
			if len(p) < 6 {
				continue
			}
			b := Branch{Name: p[0], Upstream: p[1], When: p[3], Subject: p[5], Worktree: inWt[p[0]]}
			b.Unix = int64(atoi(p[4]))
			b.Ahead, b.Behind, b.Gone = parseTrack(p[2])
			r.Branches = append(r.Branches, b)
			if b.Unix > r.LastUnix {
				r.LastUnix = b.Unix
			}
		}
	}
	return r
}

func parseTrack(s string) (ahead, behind int, gone bool) {
	if s == "gone" {
		return 0, 0, true
	}
	for _, part := range strings.Split(s, ", ") {
		if n, ok := strings.CutPrefix(part, "ahead "); ok {
			ahead = atoi(n)
		}
		if n, ok := strings.CutPrefix(part, "behind "); ok {
			behind = atoi(n)
		}
	}
	return
}

func worktreeStatus(wt *Worktree) {
	if out, err := git(wt.Path, "status", "--porcelain=v2", "--branch"); err == nil {
		for _, l := range strings.Split(out, "\n") {
			switch {
			case strings.HasPrefix(l, "# branch.upstream "):
				wt.Upstream = strings.TrimPrefix(l, "# branch.upstream ")
			case strings.HasPrefix(l, "# branch.ab "):
				var a, b int
				for _, f := range strings.Fields(strings.TrimPrefix(l, "# branch.ab ")) {
					if strings.HasPrefix(f, "+") {
						a = atoi(f[1:])
					} else if strings.HasPrefix(f, "-") {
						b = atoi(f[1:])
					}
				}
				wt.Ahead, wt.Behind = a, b
			case l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, "!"):
			default:
				wt.Changes++
			}
		}
	}
	if out, err := git(wt.Path, "log", "-1", "--format=%h%x1f%s%x1f%cr%x1f%an%x1f%ct"); err == nil {
		p := strings.Split(strings.TrimSpace(out), sep)
		if len(p) == 5 {
			wt.Last = Commit{Hash: p[0], Subject: p[1], When: p[2], Author: p[3], Unix: int64(atoi(p[4]))}
		}
	}
}

func loadPRs(repo string) ([]PR, error) {
	out, err := sysx.Run(20*time.Second, "gh", "pr", "list", "-R", repo, "--state", "open", "--limit", "50",
		"--json", "number,title,headRefName,state,url,isDraft,reviewDecision")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Number         int    `json:"number"`
		Title          string `json:"title"`
		HeadRefName    string `json:"headRefName"`
		State          string `json:"state"`
		URL            string `json:"url"`
		IsDraft        bool   `json:"isDraft"`
		ReviewDecision string `json:"reviewDecision"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	prs := make([]PR, 0, len(raw))
	for _, p := range raw {
		prs = append(prs, PR{Number: p.Number, Title: p.Title, Head: p.HeadRefName, State: p.State, URL: p.URL, Draft: p.IsDraft, Review: p.ReviewDecision})
	}
	sort.Slice(prs, func(i, j int) bool { return prs[i].Number > prs[j].Number })
	return prs, nil
}

// Graph returnerar en textgraf över repots senaste commits på alla branches.
func Graph(path string, n int) (string, error) {
	return git(path, "log", "--graph", "--all", "--date=relative", "-n", itoa(n),
		"--format=%h %d %s (%cr, %an)", "--color=never")
}
