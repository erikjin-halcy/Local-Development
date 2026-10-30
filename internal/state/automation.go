package state

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type AutomationTask struct {
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	Enabled  bool   `json:"enabled"`
	Running  bool   `json:"running"`
	LastRun  *struct {
		Start string `json:"start"`
		Exit  *int   `json:"exit"`
	} `json:"lastRun"`
}

type Automation struct {
	URL      string           `json:"url"`
	CloudURL string           `json:"cloudURL"`
	Online   bool             `json:"online"`
	Tasks    []AutomationTask `json:"tasks"`
	CanStart bool             `json:"canStart"`
}

var automationClient = &http.Client{Timeout: 800 * time.Millisecond}

// collectAutomation läser uppgifterna från Local-Automations lokala API.
func collectAutomation(url, cloudURL string, canStart bool) Automation {
	a := Automation{URL: url, CloudURL: cloudURL, Tasks: []AutomationTask{}, CanStart: canStart}
	if url == "" {
		return a
	}
	r, err := automationClient.Get(strings.TrimRight(url, "/") + "/api/tasks")
	if err != nil {
		return a
	}
	defer r.Body.Close()
	if r.StatusCode == 200 && json.NewDecoder(r.Body).Decode(&a.Tasks) == nil {
		a.Online = true
	}
	return a
}
