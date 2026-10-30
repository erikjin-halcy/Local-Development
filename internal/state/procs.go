package state

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

type Server struct {
	Port      int      `json:"port"`
	Addrs     []string `json:"addrs"`
	PID       int      `json:"pid"`
	Process   string   `json:"process"`
	Command   string   `json:"command"`
	Cwd       string   `json:"cwd"`
	Kind      string   `json:"kind"`
	Local     bool     `json:"local"`
	Worktree  *Ref     `json:"worktree,omitempty"`
	Pane      string   `json:"pane,omitempty"`
	Container string   `json:"container,omitempty"`
	// Systemappar (Discord, Notion …) visas bara på begäran
	App bool `json:"app"`
}

type proc struct {
	ppid    int
	command string
}

func processTable() map[int]proc {
	out, err := sysx.Run(5*time.Second, "ps", "-axww", "-o", "pid=,ppid=,command=")
	if err != nil {
		return nil
	}
	t := map[int]proc{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		t[atoi(f[0])] = proc{ppid: atoi(f[1]), command: strings.Join(f[2:], " ")}
	}
	return t
}

func listeners() map[[2]int]*Server {
	out, _ := sysx.Run(8*time.Second, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcn")
	res := map[[2]int]*Server{}
	var pid int
	var cmd string
	for _, l := range strings.Split(out, "\n") {
		if l == "" {
			continue
		}
		switch l[0] {
		case 'p':
			pid = atoi(l[1:])
		case 'c':
			cmd = l[1:]
		case 'n':
			addr := l[1:]
			i := strings.LastIndex(addr, ":")
			if i < 0 {
				continue
			}
			port := atoi(addr[i+1:])
			key := [2]int{pid, port}
			s := res[key]
			if s == nil {
				s = &Server{Port: port, PID: pid, Process: strings.ReplaceAll(cmd, `\x20`, " ")}
				res[key] = s
			}
			host := addr[:i]
			s.Addrs = append(s.Addrs, host)
			if host == "127.0.0.1" || host == "[::1]" || host == "localhost" {
				s.Local = true
			}
		}
	}
	return res
}

func cwds(pids []int) map[int]string {
	res := map[int]string{}
	if len(pids) == 0 {
		return res
	}
	list := make([]string, len(pids))
	for i, p := range pids {
		list[i] = itoa(p)
	}
	out, _ := sysx.Run(8*time.Second, "lsof", "-a", "-d", "cwd", "-Fn", "-p", strings.Join(list, ","))
	var pid int
	for _, l := range strings.Split(out, "\n") {
		if l == "" {
			continue
		}
		switch l[0] {
		case 'p':
			pid = atoi(l[1:])
		case 'n':
			res[pid] = l[1:]
		}
	}
	return res
}

var kinds = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"docker", regexp.MustCompile(`(?i)orbstack|com\.docker|vpnkit|docker-proxy`)},
	{"frontend", regexp.MustCompile(`(?i)\bvite\b|next(-server| dev| start)|webpack|react-scripts|\bastro\b|\bnuxt\b|svelte-kit|remix|\bexpo\b|storybook|parcel|ng serve|metro`)},
	{"database", regexp.MustCompile(`(?i)\bpostgres\b|mysqld|redis-server|mongod|clickhouse|cockroach|surreal`)},
	{"backend", regexp.MustCompile(`(?i)uvicorn|gunicorn|hypercorn|flask|django|manage\.py|fastapi|\bnest\b|express|nodemon|tsx (watch|src)|ts-node|go-build|\bair\b|rails|puma|cargo run|\bdeno\b|\bbun\b|server\.(m?js|ts|py)|api\.(m?js|ts|py)|app\.py|main\.py|\.mjs|wrangler|supabase|firebase`)},
	{"ai", regexp.MustCompile(`(?i)ollama|lm ?studio|llama`)},
}

var appRe = regexp.MustCompile(`(?i)^(ControlCe|rapportd|Discord|Notion|Linear|Code\\x20H|Code Helper|Spotify|Slack|Figma|Raycast|Dropbox|Google|Chrome|Cursor|Electron|sharingd|Adobe|zoom|WhatsApp|Teams|AirPlay|identitys|com\.apple)`)

func classify(s *Server) {
	text := s.Process + " " + s.Command
	for _, k := range kinds {
		if k.re.MatchString(text) {
			s.Kind = k.kind
			return
		}
	}
	if s.Worktree != nil {
		s.Kind = "dev"
		return
	}
	s.Kind = "app"
	s.App = appRe.MatchString(s.Process) || !strings.HasPrefix(s.Cwd, "/Users/") ||
		strings.HasPrefix(s.Command, "/Applications/") || strings.HasPrefix(s.Command, "/System/")
}

func collectServers(paneByPID map[int]string) []Server {
	table := processTable()
	ls := listeners()
	var pids []int
	seen := map[int]bool{}
	for k := range ls {
		if !seen[k[0]] {
			seen[k[0]] = true
			pids = append(pids, k[0])
		}
	}
	dirs := cwds(pids)
	out := make([]Server, 0, len(ls))
	for _, s := range ls {
		s.Command = table[s.PID].command
		s.Cwd = dirs[s.PID]
		// Gå uppåt i processträdet tills vi hittar en tmux-pane
		for p, i := s.PID, 0; p > 1 && i < 30; p, i = table[p].ppid, i+1 {
			if id, ok := paneByPID[p]; ok {
				s.Pane = id
				break
			}
		}
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Port < out[j].Port })
	return out
}

// ---------- Docker / OrbStack ----------

type Container struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Image    string `json:"image"`
	State    string `json:"state"`
	Status   string `json:"status"`
	Ports    string `json:"ports"`
	Project  string `json:"project"`
	Service  string `json:"service"`
	Dir      string `json:"dir"`
	Worktree *Ref   `json:"worktree,omitempty"`
	// Publicerade portar på värddatorn
	HostPorts []int `json:"hostPorts"`
}

type Docker struct {
	Context    string      `json:"context"`
	Error      string      `json:"error,omitempty"`
	Containers []Container `json:"containers"`
}

var hostPortRe = regexp.MustCompile(`(?:[\d.]+|\[::\]|::):(\d+)->`)

func collectDocker() Docker {
	d := Docker{Containers: []Container{}}
	if !sysx.Has("docker") {
		d.Error = "docker saknas"
		return d
	}
	if out, err := sysx.Run(3*time.Second, "docker", "context", "show"); err == nil {
		d.Context = strings.TrimSpace(out)
	}
	out, err := sysx.Run(6*time.Second, "docker", "ps", "-a", "--no-trunc", "--format", "{{json .}}")
	if err != nil {
		d.Error = err.Error()
		return d
	}
	var rawRows []map[string]any
	var ids []string
	for _, l := range strings.Split(out, "\n") {
		var raw map[string]any
		if l == "" || json.Unmarshal([]byte(l), &raw) != nil {
			continue
		}
		rawRows = append(rawRows, raw)
		ids = append(ids, str(raw["ID"]))
	}
	// docker ps visar inte alltid etiketterna; docker inspect gör det
	allLabels := map[string]map[string]string{}
	if len(ids) > 0 {
		out, err := sysx.Run(6*time.Second, "docker", append([]string{"inspect", "--format", "{{.Id}}\x1f{{json .Config.Labels}}"}, ids...)...)
		if err == nil {
			for _, l := range strings.Split(out, "\n") {
				id, js, ok := strings.Cut(l, "\x1f")
				if !ok {
					continue
				}
				m := map[string]string{}
				_ = json.Unmarshal([]byte(js), &m)
				allLabels[id] = m
			}
		}
	}
	for _, r := range rawRows {
		raw := map[string]string{}
		for k, v := range r {
			raw[k] = str(v)
		}
		labels := allLabels[raw["ID"]]
		if labels == nil {
			labels = map[string]string{}
		}
		c := Container{ID: raw["ID"], Name: raw["Names"], Image: raw["Image"], State: raw["State"],
			Status: raw["Status"], Ports: raw["Ports"], HostPorts: []int{},
			Project: labels["com.docker.compose.project"], Service: labels["com.docker.compose.service"],
			Dir: labels["com.docker.compose.project.working_dir"]}
		if len(c.ID) > 12 {
			c.ID = c.ID[:12]
		}
		if sp := labels["com.supabase.cli.project"]; sp != "" {
			c.Project = "supabase · " + sp
			if c.Dir == "" {
				c.Dir = labels["com.supabase.cli.workdir"]
			}
		}
		// Supabase-containrar utan etiketter (t.ex. egna stubbar) hör till projektet via nätverket
		if c.Project == "" {
			if sp, ok := strings.CutPrefix(raw["Networks"], "supabase_network_"); ok && !strings.Contains(sp, ",") {
				c.Project = "supabase · " + sp
			}
		}
		seen := map[int]bool{}
		for _, m := range hostPortRe.FindAllStringSubmatch(c.Ports, -1) {
			if p := atoi(m[1]); !seen[p] {
				seen[p] = true
				c.HostPorts = append(c.HostPorts, p)
			}
		}
		d.Containers = append(d.Containers, c)
	}
	dirs := map[string]string{}
	for _, c := range d.Containers {
		if c.Dir != "" && c.Project != "" {
			dirs[c.Project] = c.Dir
		}
	}
	for i := range d.Containers {
		if d.Containers[i].Dir == "" {
			d.Containers[i].Dir = dirs[d.Containers[i].Project]
		}
	}
	sort.Slice(d.Containers, func(i, j int) bool {
		if d.Containers[i].Project != d.Containers[j].Project {
			return d.Containers[i].Project < d.Containers[j].Project
		}
		return d.Containers[i].Name < d.Containers[j].Name
	})
	return d
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
