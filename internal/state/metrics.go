package state

// Datorns belastning över tid: en mätning i minuten (CPU, minne, energi, batteri, temperatur och
// de processer som drar mest), sparad lokalt som en jsonl-fil per dag i ~/.cache/devctl/metrics.
// Inget av detta skickas någonstans; det är till för att se vad som drog och när.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/config"
	"github.com/erikjin-halcy/Local-Development/internal/sysx"
)

type Metric struct {
	T        int64        `json:"t"`        // unix-sekunder
	CPU      float64      `json:"cpu"`      // user+sys i procent av hela datorn (iostat, en sekund)
	Load     float64      `json:"load"`     // load average, 1 minut
	MemUsed  float64      `json:"memUsed"`  // GB använt: aktivt, fastlåst och komprimerat (vm_stat)
	MemTotal float64      `json:"memTotal"` // GB
	Swap     float64      `json:"swap"`     // GB swap använt
	MemFree  int          `json:"memFree"`  // procent fritt enligt memory_pressure
	Pressure int          `json:"pressure"` // 1 normal, 2 varning, 4 kritisk (kern.memorystatus_vm_pressure_level)
	Power    float64      `json:"power"`    // summerad energipåverkan för de största processerna, -1 när den inte mättes
	GPU      int          `json:"gpu"`      // GPU-användning i procent, -1 om okänd
	Battery  int          `json:"battery"`  // procent, -1 utan batteri
	Charging bool         `json:"charging"`
	AC       bool         `json:"ac"`
	Thermal  string       `json:"thermal,omitempty"` // tomt när macOS inte rapporterar någon termisk begränsning
	Procs    []MetricProc `json:"procs"`
}

type MetricProc struct {
	PID   int     `json:"pid"`
	Name  string  `json:"name"`
	CPU   float64 `json:"cpu"`             // medel sedan förra mätningen, i procent av en kärna
	Power float64 `json:"power"`           // energipåverkan, -1 när den inte mättes
	Mem   int     `json:"mem"`             // MB
	Where string  `json:"where,omitempty"` // repo@branch, pane eller server när devctl vet det
}

const (
	metricsKeepDays = 14
	// top kostar ungefär en sekund CPU-tid (den går igenom alla processer två gånger), så
	// energipåverkan mäts bara var 10:e minut; CPU per process räknas billigt med ps varje minut.
	powerEvery = 10 * time.Minute
)

func MetricsDir() string { return filepath.Join(config.CacheDir(), "metrics") }

func metricsFile(day time.Time) string {
	return filepath.Join(MetricsDir(), day.Format("2006-01-02")+".jsonl")
}

var (
	iostatRe = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\d+)\s+([\d.]+)`)
	pageRe   = regexp.MustCompile(`page size of (\d+) bytes`)
	vmRe     = regexp.MustCompile(`(?m)^(Pages active|Pages wired down|Pages occupied by compressor):\s+(\d+)`)
	freeRe   = regexp.MustCompile(`free percentage: (\d+)%`)
	swapRe   = regexp.MustCompile(`used = ([\d.]+)M`)
	battRe   = regexp.MustCompile(`(\d+)%; ([^;]+);`)
	gpuRe    = regexp.MustCompile(`"Device Utilization %"=(\d+)`)
	appDirRe = regexp.MustCompile(`/([^/]+)\.app/`)
	claudeV  = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	psRe     = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\S+)\s+(\d+)\s+(.+?)\s*$`)
)

// Förra mätningens CPU-tid per process, för att räkna medel-CPU över minuten
var (
	prevCPU   map[int]float64
	prevAt    time.Time
	lastPower time.Time
)

type psRow struct {
	pid, ppid int
	cpu       float64 // sekunder sedan processen startade
	rss       int     // KB
	comm      string
}

// CollectMetric gör en mätning. Varje minut startar den sju små program som tillsammans tar
// några hundradelar CPU-sekund; var 10:e minut även top (ungefär en sekund) för energipåverkan.
// snap används för att säga vad processerna hör till.
func CollectMetric(snap Snapshot) Metric {
	m := Metric{T: time.Now().Unix(), GPU: -1, Battery: -1, Power: -1}
	if b, err := sysx.Run(5*time.Second, "iostat", "-n", "0", "-c", "2", "-w", "1"); err == nil {
		lines := strings.Split(strings.TrimSpace(b), "\n")
		if r := iostatRe.FindStringSubmatch(lines[len(lines)-1]); r != nil {
			us, _ := strconv.Atoi(r[1])
			sy, _ := strconv.Atoi(r[2])
			m.CPU = float64(us + sy)
			m.Load = atof(r[4])
		}
	}
	if b, err := sysx.Run(2*time.Second, "vm_stat"); err == nil {
		page := 4096.0
		if r := pageRe.FindStringSubmatch(b); r != nil {
			page = atof(r[1])
		}
		pages := 0.0
		for _, r := range vmRe.FindAllStringSubmatch(b, -1) {
			pages += atof(r[2])
		}
		m.MemUsed = round1(pages * page / (1 << 30))
	}
	if b, err := sysx.Run(2*time.Second, "sysctl", "-n", "hw.memsize", "vm.swapusage", "kern.memorystatus_vm_pressure_level"); err == nil {
		lines := strings.Split(strings.TrimSpace(b), "\n")
		if len(lines) > 0 {
			m.MemTotal = round1(atof(strings.TrimSpace(lines[0])) / (1 << 30))
		}
		if len(lines) > 1 {
			if r := swapRe.FindStringSubmatch(lines[1]); r != nil {
				m.Swap = round1(atof(r[1]) / 1024)
			}
		}
		if len(lines) > 2 {
			m.Pressure, _ = strconv.Atoi(strings.TrimSpace(lines[2]))
		}
	}
	if b, err := sysx.Run(3*time.Second, "memory_pressure", "-Q"); err == nil {
		if r := freeRe.FindStringSubmatch(b); r != nil {
			m.MemFree, _ = strconv.Atoi(r[1])
		}
	}
	if b, err := sysx.Run(3*time.Second, "pmset", "-g", "batt"); err == nil {
		m.AC = strings.Contains(b, "AC Power")
		if r := battRe.FindStringSubmatch(b); r != nil {
			m.Battery, _ = strconv.Atoi(r[1])
			st := strings.TrimSpace(r[2])
			m.Charging = st == "charging" || st == "finishing charge"
		}
	}
	if b, err := sysx.Run(3*time.Second, "pmset", "-g", "therm"); err == nil {
		for _, l := range strings.Split(b, "\n") {
			l = strings.TrimSpace(l)
			// "Note: No thermal warning level has been recorded" betyder att allt är normalt
			if l != "" && !strings.HasPrefix(l, "Note:") && !strings.HasPrefix(l, "No ") {
				m.Thermal = strings.TrimSpace(m.Thermal + " " + l)
			}
		}
	}
	if b, err := sysx.Run(3*time.Second, "ioreg", "-r", "-d", "1", "-c", "IOAccelerator"); err == nil {
		if r := gpuRe.FindStringSubmatch(b); r != nil {
			m.GPU, _ = strconv.Atoi(r[1])
		}
	}

	rows := map[int]psRow{}
	if b, err := sysx.Run(5*time.Second, "ps", "-axo", "pid=,ppid=,time=,rss=,comm="); err == nil {
		for _, l := range strings.Split(b, "\n") {
			if r := psRe.FindStringSubmatch(l); r != nil {
				pid, _ := strconv.Atoi(r[1])
				ppid, _ := strconv.Atoi(r[2])
				rss, _ := strconv.Atoi(r[4])
				rows[pid] = psRow{pid, ppid, cpuSeconds(r[3]), rss, r[5]}
			}
		}
	}
	// Energipåverkan per process finns bara i top
	power := map[int]float64{}
	if time.Since(lastPower) >= powerEvery {
		lastPower = time.Now()
		out, _ := sysx.Run(10*time.Second, "top", "-l", "2", "-s", "1", "-o", "power", "-n", "10", "-stats", "pid,power")
		if i := strings.LastIndex(out, "PID"); i >= 0 {
			m.Power = 0
			for _, l := range strings.Split(out[i:], "\n")[1:] {
				f := strings.Fields(l)
				if len(f) != 2 {
					continue
				}
				pid, err := strconv.Atoi(f[0])
				if err != nil || rows[pid].ppid == os.Getpid() {
					continue
				}
				power[pid] = atof(f[1])
				m.Power += power[pid]
			}
			m.Power = round1(m.Power)
		}
	}
	m.Procs = topProcs(rows, power, snap)
	return m
}

// topProcs tar de tio processer som drog mest CPU sedan förra mätningen (och de som drog mest
// energi, när den mättes) och ger dem läsbara namn: hjälpprocesser får sin apps namn (Notion
// Helper (Renderer) blir Notion) och Claude Code (som heter som sin version) sessionens namn.
func topProcs(rows map[int]psRow, power map[int]float64, snap Snapshot) []MetricProc {
	now := time.Now()
	cur := make(map[int]float64, len(rows))
	for pid, r := range rows {
		cur[pid] = r.cpu
	}
	dt := now.Sub(prevAt).Seconds()
	prev := prevCPU
	prevCPU, prevAt = cur, now
	// Första mätningen eller efter viloläge: inget att jämföra med
	if prev == nil || dt > 180 || dt < 1 {
		prev = nil
	}
	type cand struct {
		pid int
		cpu float64
	}
	var cands []cand
	for pid, r := range rows {
		c := 0.0
		if prev != nil {
			if p, ok := prev[pid]; ok {
				c = (r.cpu - p) / dt * 100
			} else {
				c = r.cpu / dt * 100 // startade under minuten
			}
		}
		if _, ok := power[pid]; ok || c > 0.05 {
			cands = append(cands, cand{pid, c})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].cpu > cands[j].cpu })

	claude := map[int]string{}
	for _, s := range ClaudeSessions() {
		claude[s.PID] = s.Name
	}
	panes := map[int]string{}
	for _, s := range snap.Sessions {
		for _, w := range s.Windows {
			for _, p := range w.Panes {
				label := p.Title
				if label == "" {
					label = s.Name
				}
				panes[p.PID] = "pane " + label
			}
		}
	}
	servers := map[int]string{}
	for _, s := range snap.Servers {
		where := ":" + strconv.Itoa(s.Port)
		if s.Worktree != nil {
			where = s.Worktree.Repo + "@" + s.Worktree.Branch + " " + where
		}
		servers[s.PID] = where
	}

	var procs []MetricProc
	for i, c := range cands {
		pw, hasPower := power[c.pid]
		if i >= 10 && !hasPower {
			continue
		}
		r := rows[c.pid]
		p := MetricProc{PID: c.pid, CPU: round1(c.cpu), Power: -1, Mem: r.rss / 1024, Name: filepath.Base(r.comm)}
		if hasPower {
			p.Power = pw
		}
		if a := appDirRe.FindStringSubmatch(r.comm); a != nil {
			p.Name = a[1]
		}
		// Vad processen hör till: närmaste förälder som är en Claude-session, en server eller en pane
		for cur, hops := c.pid, 0; cur > 1 && hops < 12; hops++ {
			if name, ok := claude[cur]; ok {
				if cur == c.pid {
					p.Name = "Claude (" + name + ")"
				} else if p.Where == "" {
					p.Where = "Claude (" + name + ")"
				}
			}
			if s, ok := servers[cur]; ok && p.Where == "" {
				p.Where = s
			}
			if s, ok := panes[cur]; ok && p.Where == "" {
				p.Where = s
			}
			r, ok := rows[cur]
			if !ok {
				break
			}
			cur = r.ppid
		}
		if claudeV.MatchString(p.Name) {
			p.Name = "Claude"
		}
		procs = append(procs, p)
	}
	return procs
}

// cpuSeconds tolkar ps-kolumnen time: [[dd-]hh:]mm:ss.ss
func cpuSeconds(s string) float64 {
	days := 0.0
	if d, rest, ok := strings.Cut(s, "-"); ok {
		days, s = atof(d), rest
	}
	total := 0.0
	for _, part := range strings.Split(s, ":") {
		total = total*60 + atof(part)
	}
	return days*86400 + total
}

// SaveMetric lägger mätningen sist i dagens fil och tar bort filer äldre än 14 dagar.
func SaveMetric(m Metric) error {
	_ = os.MkdirAll(MetricsDir(), 0o755)
	b, _ := json.Marshal(m)
	f, err := os.OpenFile(metricsFile(time.Unix(m.T, 0)), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	f.Close()
	cut := time.Now().AddDate(0, 0, -metricsKeepDays).Format("2006-01-02")
	files, _ := filepath.Glob(filepath.Join(MetricsDir(), "*.jsonl"))
	for _, p := range files {
		if strings.TrimSuffix(filepath.Base(p), ".jsonl") < cut {
			_ = os.Remove(p)
		}
	}
	return err
}

// LoadMetrics läser mätningarna från och med since, äldst först.
func LoadMetrics(since time.Time) []Metric {
	var out []Metric
	for d := since.Truncate(24*time.Hour).AddDate(0, 0, -1); !d.After(time.Now()); d = d.AddDate(0, 0, 1) {
		f, err := os.Open(metricsFile(d))
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 1<<16), 1<<20)
		for sc.Scan() {
			var m Metric
			if json.Unmarshal(sc.Bytes(), &m) == nil && m.T >= since.Unix() {
				out = append(out, m)
			}
		}
		f.Close()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

// MetricPoint är en punkt i diagrammen; för längre perioder är den ett medel över flera mätningar.
type MetricPoint struct {
	T        int64   `json:"t"`
	CPU      float64 `json:"cpu"`
	Load     float64 `json:"load"`
	MemUsed  float64 `json:"memUsed"`
	Swap     float64 `json:"swap"`
	MemFree  int     `json:"memFree"`
	Pressure int     `json:"pressure"` // det värsta under punkten
	Power    float64 `json:"power"`
	GPU      int     `json:"gpu"`
	Battery  int     `json:"battery"`
	Charging bool    `json:"charging"`
	Thermal  string  `json:"thermal,omitempty"`
}

// ProcTotal är en process (eller app) summerad över en period.
type ProcTotal struct {
	Name  string  `json:"name"`
	Where string  `json:"where,omitempty"`
	CPU   float64 `json:"cpu"`   // medel-CPU över perioden, i procent av en kärna
	Power float64 `json:"power"` // medel-energipåverkan över de mätningar som hade den, -1 om ingen
	Mem   int     `json:"mem"`   // högsta minne, MB
	Seen  int     `json:"seen"`  // antal mätningar där den var bland de största
}

// MetricPoints slår ihop mätningarna till punkter om step sekunder (0 = varje mätning).
func MetricPoints(ms []Metric, step int64) []MetricPoint {
	var out []MetricPoint
	var cur *MetricPoint
	n, np := 0, 0
	flush := func() {
		if cur == nil {
			return
		}
		f := float64(n)
		cur.CPU, cur.Load, cur.MemUsed, cur.Swap = round1(cur.CPU/f), round1(cur.Load/f), round1(cur.MemUsed/f), round1(cur.Swap/f)
		// Energin mäts bara ibland; -1 när ingen mätning i punkten hade den
		if np > 0 {
			cur.Power = round1(cur.Power / float64(np))
		} else {
			cur.Power = -1
		}
		cur.MemFree, cur.GPU = int(float64(cur.MemFree)/f+0.5), int(float64(cur.GPU)/f+0.5)
		out = append(out, *cur)
		cur = nil
	}
	for _, m := range ms {
		t := m.T
		if step > 0 {
			t = m.T - m.T%step
		}
		if cur != nil && cur.T != t {
			flush()
		}
		if cur == nil {
			cur, n, np = &MetricPoint{T: t}, 0, 0
		}
		n++
		cur.CPU += m.CPU
		cur.Load += m.Load
		cur.MemUsed += m.MemUsed
		cur.Swap += m.Swap
		cur.MemFree += m.MemFree
		if m.Power >= 0 {
			cur.Power += m.Power
			np++
		}
		cur.GPU += m.GPU
		if m.Pressure > cur.Pressure {
			cur.Pressure = m.Pressure
		}
		// Batteriet och laddningen: det senaste värdet i punkten
		cur.Battery, cur.Charging = m.Battery, m.Charging
		if m.Thermal != "" {
			cur.Thermal = m.Thermal
		}
	}
	flush()
	return out
}

// MetricGaps är perioder utan mätningar, när datorn sov eller var avstängd (eller devctl inte körde).
func MetricGaps(ms []Metric) [][2]int64 {
	var out [][2]int64
	for i := 1; i < len(ms); i++ {
		if ms[i].T-ms[i-1].T > 150 {
			out = append(out, [2]int64{ms[i-1].T, ms[i].T})
		}
	}
	return out
}

// TopProcs summerar processerna i mätningarna, de som drog mest CPU först. Processer som inte var
// bland de största i en mätning räknas som 0 där, så medlet är en undre gräns.
func TopProcs(ms []Metric, limit int) []ProcTotal {
	byKey := map[string]*ProcTotal{}
	withPower := 0
	for _, m := range ms {
		if m.Power >= 0 {
			withPower++
		}
		// Flera processer med samma namn och plats (t.ex. en apps hjälpprocesser) räknas ihop
		seen := map[string]bool{}
		for _, p := range m.Procs {
			k := p.Name + "\x00" + p.Where
			t := byKey[k]
			if t == nil {
				t = &ProcTotal{Name: p.Name, Where: p.Where}
				byKey[k] = t
			}
			t.CPU += p.CPU
			if p.Power > 0 {
				t.Power += p.Power
			}
			if p.Mem > t.Mem {
				t.Mem = p.Mem
			}
			if !seen[k] {
				t.Seen++
				seen[k] = true
			}
		}
	}
	out := make([]ProcTotal, 0, len(byKey))
	for _, t := range byKey {
		if n := float64(len(ms)); n > 0 {
			t.CPU = round1(t.CPU / n)
		}
		if withPower > 0 {
			t.Power = round1(t.Power / float64(withPower))
		} else {
			t.Power = -1
		}
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		return out[i].Power > out[j].Power
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func atof(s string) float64 { f, _ := strconv.ParseFloat(s, 64); return f }

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }
