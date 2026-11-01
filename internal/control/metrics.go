package control

// Fliken Dator: datorns belastning över tid. Mätningen görs en gång i minuten oavsett om någon
// tittar, och sparas bara lokalt (se state.SaveMetric); den ingår inte i molnsammanfattningen.

import (
	"log"
	"time"

	"github.com/erikjin-halcy/Local-Development/internal/state"
)

// StartMetrics mäter en gång i minuten. Ingen mätning görs medan datorn sover; glappet syns i diagrammet.
func (c *Controller) StartMetrics() {
	go func() {
		time.Sleep(20 * time.Second) // låt tmux och servrar läsas in först, så att processerna får sin plats
		for {
			if err := state.SaveMetric(state.CollectMetric(c.col.Snapshot())); err != nil {
				log.Println("mätning:", err)
			}
			time.Sleep(time.Minute)
		}
	}()
}

// metrics: range 24h eller 7d. Med from och to (unix-sekunder) bara processerna under den tiden.
func (c *Controller) metrics(a Args) (any, error) {
	span, step := 24*time.Hour, int64(0)
	if a.S("range") == "7d" {
		span, step = 7*24*time.Hour, 600
	}
	if from, to := int64(a.I("from", 0)), int64(a.I("to", 0)); from > 0 && to >= from {
		ms := state.LoadMetrics(time.Unix(from, 0))
		var in []state.Metric
		for _, m := range ms {
			if m.T <= to {
				in = append(in, m)
			}
		}
		return map[string]any{"procs": state.TopProcs(in, 8), "samples": len(in)}, nil
	}
	ms := state.LoadMetrics(time.Now().Add(-span))
	res := map[string]any{"range": a.S("range"), "step": step, "points": state.MetricPoints(ms, step), "gaps": state.MetricGaps(ms),
		"top": state.TopProcs(ms, 15), "samples": len(ms)}
	if len(ms) > 0 {
		last := ms[len(ms)-1]
		res["now"] = last
		res["memTotal"] = last.MemTotal
		res["first"] = ms[0].T
	}
	return res, nil
}
