package control

// Claude-kontext per repo. Varje repo har en mapp i Obsidian-valvet, <projektmapp>/<repo>/Claude/, med:
//   - CLAUDE.md: personliga instruktioner för repot, inlästa av Claude Code i alla repots worktrees
//     via en CLAUDE.local.md som devctl lägger i worktreen (och utesluter från git).
//   - Sessioner/: en sammanfattning per Claude-session i repot, uppdaterad högst en gång i timmen
//     och bara medan sessionen är aktiv (dess logg har ändrats den senaste timmen).
//
// Claude Codes @import klarar inte mellanslag i sökvägar, så importen går via en länk utan
// mellanslag: ~/.config/devctl/context/<repo> → valvets Claude-mapp.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/state"
)

const (
	sessionEvery  = time.Hour // uppdatera en aktiv sessions kontext högst så här ofta
	sessionActive = time.Hour // en session är aktiv om dess logg ändrats inom så här lång tid
	localMarker   = "<!-- devctl:claude-context -->"
)

var (
	spaceRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
	// Tecken som inte får finnas i filnamn (eller ställer till det i Obsidian); å, ä och ö behålls
	unsafeName = regexp.MustCompile(`[/\\:*?"<>|#^\[\]\n\r\t]+`)
)

func contextLink(repo string) string {
	return filepath.Join(config.Dir(), "context", spaceRe.ReplaceAllString(repo, "-"))
}

func claudeDir(cfg config.Config, repo string) string {
	d := state.RepoDir(cfg, repo)
	if d == "" {
		return ""
	}
	return filepath.Join(d, "Claude")
}

func claudeTemplate(repo, link string) string {
	return fmt.Sprintf(`# Personliga instruktioner för %s

<!-- Läses in av Claude Code i alla worktrees av %s via CLAUDE.local.md (skapad av devctl).
     Skriv här det Claude ska veta när du jobbar i repot: mål, arbetssätt, saker att undvika. -->

## Om projektet


## Arbetssätt


## Tidigare sessioner
Sammanfattningar av tidigare Claude-sessioner i repot finns i `+"`%s/Sessioner/`"+`.
Läs den senaste när du fortsätter ett påbörjat arbete.
`, repo, repo, link)
}

// ensureClaudeContext skapar repots Claude-mapp, länken utan mellanslag och CLAUDE.local.md i
// varje worktree. En befintlig CLAUDE.local.md som inte skapats av devctl lämnas orörd.
func (c *Controller) ensureClaudeContext(r state.Repo) error {
	dir := claudeDir(c.cfg, r.Name)
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(dir, "Sessioner"), 0o755); err != nil {
		return err
	}
	link := contextLink(r.Name)
	if cur, err := os.Readlink(link); err != nil || cur != dir {
		_ = os.MkdirAll(filepath.Dir(link), 0o755)
		_ = os.Remove(link)
		if err := os.Symlink(dir, link); err != nil {
			return err
		}
	}
	short := "~" + strings.TrimPrefix(link, config.Home("~"))
	md := filepath.Join(dir, "CLAUDE.md")
	if _, err := os.Stat(md); os.IsNotExist(err) {
		if err := os.WriteFile(md, []byte(claudeTemplate(r.Name, short)), 0o644); err != nil {
			return err
		}
	}
	excluded := false
	for _, wt := range r.Worktrees {
		if wt.Missing {
			continue
		}
		local := filepath.Join(wt.Path, "CLAUDE.local.md")
		if b, err := os.ReadFile(local); err == nil && !bytes.Contains(b, []byte(localMarker)) {
			continue // användarens egen fil
		}
		var body strings.Builder
		fmt.Fprintf(&body, "%s\n<!-- Skapad av devctl. Committas inte (står i .git/info/exclude).\n     Dina personliga instruktioner för %s ligger i Obsidian och läses in här: -->\n@%s/CLAUDE.md\n", localMarker, r.Name, short)
		// En CLAUDE.local.md gör att Claude slutar läsa AGENTS.md om repot inte har en CLAUDE.md
		if _, err := os.Stat(filepath.Join(wt.Path, "AGENTS.md")); err == nil {
			if _, err := os.Stat(filepath.Join(wt.Path, "CLAUDE.md")); err != nil {
				body.WriteString("@AGENTS.md\n")
			}
		}
		if b, _ := os.ReadFile(local); string(b) != body.String() {
			if err := os.WriteFile(local, []byte(body.String()), 0o644); err != nil {
				return err
			}
		}
		if !excluded {
			if err := excludeFromGit(wt.Path, "CLAUDE.local.md"); err != nil {
				return err
			}
			excluded = true
		}
	}
	return nil
}

// excludeFromGit lägger till ett mönster i repots .git/info/exclude (gäller alla worktrees).
func excludeFromGit(wtPath, pattern string) error {
	common, err := git(wtPath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return err
	}
	path := filepath.Join(strings.TrimSpace(common), "info", "exclude")
	b, _ := os.ReadFile(path)
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == pattern {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	prefix := ""
	if len(b) > 0 && !bytes.HasSuffix(b, []byte("\n")) {
		prefix = "\n"
	}
	_, err = fmt.Fprintf(f, "%s# devctl: personliga Claude-instruktioner\n%s\n", prefix, pattern)
	return err
}

// ---------- sessioner ----------

type session struct {
	ID, Cwd, Branch, Title, Model string
	Start, Last                   time.Time
	Prompts                       []string
	Files                         map[string]int
	Commands                      int
	Reply                         string
}

var noisePrompt = regexp.MustCompile(`^\s*(<|\[Cross-session|\[Artifact|Caveat:)`)

// parseSession läser en Claude Code-logg (jsonl). Bara raderna som behövs tolkas.
func parseSession(path string) (*session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := &session{Files: map[string]int{}}
	rd := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := rd.ReadBytes('\n')
		if len(line) > 0 {
			s.add(line)
		}
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
	}
	if s.ID == "" {
		return nil, fmt.Errorf("ingen session i %s", path)
	}
	return s, nil
}

func (s *session) add(line []byte) {
	isUser := bytes.Contains(line, []byte(`"type":"user"`))
	isAssistant := bytes.Contains(line, []byte(`"type":"assistant"`))
	isTitle := bytes.Contains(line, []byte(`"type":"ai-title"`))
	if !isUser && !isAssistant && !isTitle {
		return
	}
	var r struct {
		Type      string `json:"type"`
		SessionID string `json:"sessionId"`
		Cwd       string `json:"cwd"`
		GitBranch string `json:"gitBranch"`
		Timestamp string `json:"timestamp"`
		IsMeta    bool   `json:"isMeta"`
		AITitle   string `json:"aiTitle"`
		Message   struct {
			Model   string          `json:"model"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &r) != nil {
		return
	}
	if r.SessionID != "" {
		s.ID = r.SessionID
	}
	if r.AITitle != "" {
		s.Title = r.AITitle
	}
	// Katalogen där sessionen startades avgör repot; senare cd-kommandon i loggen gör det inte
	if r.Cwd != "" && s.Cwd == "" {
		s.Cwd = r.Cwd
		s.Branch = r.GitBranch
	}
	if t, err := time.Parse(time.RFC3339Nano, r.Timestamp); err == nil {
		if s.Start.IsZero() {
			s.Start = t
		}
		s.Last = t
	}
	switch r.Type {
	case "user":
		var text string
		if r.IsMeta || json.Unmarshal(r.Message.Content, &text) != nil {
			return // verktygssvar och systemtext är listor, inte strängar
		}
		if text = strings.TrimSpace(text); text != "" && !noisePrompt.MatchString(text) {
			s.Prompts = append(s.Prompts, text)
		}
	case "assistant":
		if r.Message.Model != "" {
			s.Model = r.Message.Model
		}
		var blocks []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Name  string `json:"name"`
			Input struct {
				FilePath string `json:"file_path"`
			} `json:"input"`
		}
		if json.Unmarshal(r.Message.Content, &blocks) != nil {
			return
		}
		for _, b := range blocks {
			switch {
			case b.Type == "text" && strings.TrimSpace(b.Text) != "":
				s.Reply = b.Text
			case b.Type == "tool_use" && b.Input.FilePath != "" && (b.Name == "Edit" || b.Name == "Write" || b.Name == "MultiEdit" || b.Name == "NotebookEdit"):
				s.Files[b.Input.FilePath]++
			case b.Type == "tool_use" && b.Name == "Bash":
				s.Commands++
			}
		}
	}
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + " …"
}

// repoForSession väljer repot: där sessionen körs, annars det där den ändrat flest filer.
func repoForSession(snap state.Snapshot, s *session) (*state.Repo, string) {
	match := func(p string) (*state.Repo, string) {
		var best *state.Repo
		bestWt, bestLen := "", 0
		for i := range snap.Repos {
			for _, wt := range snap.Repos[i].Worktrees {
				if (p == wt.Path || strings.HasPrefix(p, wt.Path+"/")) && len(wt.Path) > bestLen {
					best, bestWt, bestLen = &snap.Repos[i], wt.Path, len(wt.Path)
				}
			}
		}
		return best, bestWt
	}
	if r, wt := match(s.Cwd); r != nil {
		return r, wt
	}
	count := map[*state.Repo]int{}
	wts := map[*state.Repo]string{}
	for f, n := range s.Files {
		if r, wt := match(f); r != nil {
			count[r] += n
			wts[r] = wt
		}
	}
	var best *state.Repo
	for r, n := range count {
		if best == nil || n > count[best] {
			best = r
		}
	}
	return best, wts[best]
}

func sessionMarkdown(s *session, repo, wtPath string, now time.Time) string {
	var b strings.Builder
	title := s.Title
	if title == "" && len(s.Prompts) > 0 {
		title = clip(strings.SplitN(s.Prompts[0], "\n", 2)[0], 80)
	}
	fmt.Fprintf(&b, "---\nsession: %s\nrepo: %s\nbranch: %s\ncwd: %q\nmodell: %s\nstartad: %s\nsenast aktiv: %s\nuppdaterad: %s\ntags: [claude-session]\n---\n",
		s.ID, repo, s.Branch, s.Cwd, s.Model, s.Start.Local().Format("2006-01-02 15:04"), s.Last.Local().Format("2006-01-02 15:04"), now.Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "# %s\n\n[[%s]] · branch `%s` · %d meddelanden · %d kommandon\n\n", title, repo, s.Branch, len(s.Prompts), s.Commands)
	if len(s.Prompts) > 0 {
		b.WriteString("## Uppdraget\n" + clip(s.Prompts[0], 1200) + "\n\n")
	}
	if len(s.Prompts) > 1 {
		b.WriteString("## Senaste önskemål\n")
		start := max(1, len(s.Prompts)-6)
		for _, p := range s.Prompts[start:] {
			b.WriteString("- " + strings.ReplaceAll(clip(p, 300), "\n", " ") + "\n")
		}
		b.WriteString("\n")
	}
	if len(s.Files) > 0 {
		b.WriteString("## Ändrade filer\n")
		files := make([]string, 0, len(s.Files))
		for f := range s.Files {
			files = append(files, f)
		}
		sort.Slice(files, func(i, j int) bool { return s.Files[files[i]] > s.Files[files[j]] })
		for i, f := range files {
			if i == 25 {
				fmt.Fprintf(&b, "- … och %d till\n", len(files)-25)
				break
			}
			rel := f
			if wtPath != "" && strings.HasPrefix(f, wtPath+"/") {
				rel = strings.TrimPrefix(f, wtPath+"/")
			}
			fmt.Fprintf(&b, "- `%s` (%d)\n", rel, s.Files[f])
		}
		b.WriteString("\n")
	}
	if s.Reply != "" {
		b.WriteString("## Senaste svaret\n" + clip(s.Reply, 2000) + "\n\n")
	}
	fmt.Fprintf(&b, "## Fortsätt sessionen\n```bash\ncd %q && claude --resume %s\n```\n", s.Cwd, s.ID)
	return b.String()
}

type sessionState struct {
	File    string `json:"file"`
	Written int64  `json:"written"`
}

func sessionStatePath() string { return filepath.Join(config.CacheDir(), "sessions.json") }

// SyncSessions skriver kontexten för aktiva Claude-sessioner, högst en gång i timmen per session.
func (c *Controller) SyncSessions() (int, error) {
	if c.cfg.Obsidian.Vault == "" {
		return 0, nil
	}
	known := map[string]sessionState{}
	if b, err := os.ReadFile(sessionStatePath()); err == nil {
		_ = json.Unmarshal(b, &known)
	}
	now := time.Now()
	files, _ := filepath.Glob(config.Home("~/.claude/projects/*/*.jsonl"))
	snap := c.col.Snapshot()
	written := 0
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || now.Sub(st.ModTime()) > sessionActive {
			continue // inaktiv: uppdateras inte
		}
		id := strings.TrimSuffix(filepath.Base(f), ".jsonl")
		prev := known[id]
		if prev.Written > 0 && now.Sub(time.Unix(prev.Written, 0)) < sessionEvery {
			continue
		}
		s, err := parseSession(f)
		if err != nil {
			continue
		}
		r, wt := repoForSession(snap, s)
		if r == nil || (r.Archived && !r.Busy) {
			continue
		}
		if err := c.ensureClaudeContext(*r); err != nil {
			return written, err
		}
		// Startad utanför repot: ta branchen från worktreen där den ändrat filer
		if s.Cwd != wt && !strings.HasPrefix(s.Cwd, wt+"/") {
			for _, w := range r.Worktrees {
				if w.Path == wt {
					s.Branch = w.Branch
				}
			}
		}
		name := prev.File
		if name == "" || filepath.Dir(name) != filepath.Join(claudeDir(c.cfg, r.Name), "Sessioner") {
			label := unsafeName.ReplaceAllString(clip(s.Title, 50), " ")
			if label == "" {
				label = "session"
			}
			name = filepath.Join(claudeDir(c.cfg, r.Name), "Sessioner",
				fmt.Sprintf("%s %s (%s).md", s.Start.Local().Format("2006-01-02"), strings.TrimSpace(label), id[:8]))
		}
		if err := os.WriteFile(name, []byte(sessionMarkdown(s, r.Name, wt, now)), 0o644); err != nil {
			return written, err
		}
		known[id] = sessionState{File: name, Written: now.Unix()}
		written++
	}
	b, _ := json.MarshalIndent(known, "", "  ")
	return written, os.WriteFile(sessionStatePath(), b, 0o644)
}

// recentSessions är repots senaste sessionsfiler, nyast först.
func recentSessions(cfg config.Config, repo string, n int) []string {
	files, _ := filepath.Glob(filepath.Join(claudeDir(cfg, repo), "Sessioner", "*.md"))
	sort.Slice(files, func(i, j int) bool {
		a, _ := os.Stat(files[i])
		b, _ := os.Stat(files[j])
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	if len(files) > n {
		files = files[:n]
	}
	return files
}

func (c *Controller) syncSessionsAction(Args) (any, error) {
	if _, err := c.SyncRepoNotes(); err != nil {
		return nil, err
	}
	n, err := c.SyncSessions()
	return map[string]int{"sessions": n}, err
}
