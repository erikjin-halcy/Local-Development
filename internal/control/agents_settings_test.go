package control

import (
	"os"
	"strings"
	"testing"

	"github.com/erikjin-halcy/Local-Development/internal/config"
)

func TestAgentSettingsKeepsComments(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("DEVCTL_HOME", t.TempDir())
	_ = os.MkdirAll(config.AgentsDir(), 0o755)
	if err := os.WriteFile(config.AgentFile("test"), []byte(config.AgentTemplate("Test")), 0o644); err != nil {
		t.Fatal(err)
	}
	c := &Controller{}
	if _, err := c.agentSettings(Args{"name": "test", "every": "30m", "notify": "hög", "onlyOnChanges": "false", "repos": "Halcy-app, Dentio", "model": "sonnet"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.agentToggle(Args{"name": "test", "enabled": true}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(config.AgentFile("test"))
	s := string(b)
	for _, want := range []string{"every: 30m  # hur ofta", "notify: hög  #", "only_on_changes: false  #", `repos: ["Halcy-app", "Dentio"]  #`, `model: "sonnet"`, "enabled: true  #", "prompt: |"} {
		if !strings.Contains(s, want) {
			t.Errorf("saknar %q i\n%s", want, s)
		}
	}
	agents, errs := config.Agents()
	if len(errs) > 0 || len(agents) != 1 || agents[0].Every != "30m" || !agents[0].Enabled || agents[0].Model != "sonnet" {
		t.Fatalf("fel efter sparning: %+v %v", agents, errs)
	}
	if _, err := c.agentSettings(Args{"name": "test", "every": "1m"}); err == nil {
		t.Error("för kort schema borde ge fel")
	}
}
