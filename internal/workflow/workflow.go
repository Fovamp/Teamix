// Package workflow implements the Teamix 5-stage delivery state machine.
package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type Stage string

const (
	StageRefine  Stage = "refine"
	StageDevelop Stage = "develop"
	StageReview  Stage = "review"
	StageTest    Stage = "test"
	StageCommit  Stage = "commit"
)

var AllStages = []Stage{StageRefine, StageDevelop, StageReview, StageTest, StageCommit}

var StageLabels = map[Stage]string{
	StageRefine:  "需求细化",
	StageDevelop: "开发实现",
	StageReview:  "效果审查",
	StageTest:    "自动测试",
	StageCommit:  "入库提交",
}

type StageStatus string

const (
	StatusPending    StageStatus = "pending"
	StatusInProgress StageStatus = "in_progress"
	StatusCompleted  StageStatus = "completed"
)

type State struct {
	mu         sync.Mutex
	Current    Stage                 `json:"current"`
	Statuses   map[Stage]StageStatus `json:"statuses"`
	StageLabels  map[Stage]string    `json:"stage_labels"`
	StagePrompts map[Stage]string    `json:"stage_prompts"`
	StageOrder    []Stage            `json:"stage_order"`
	ImpactAnalysis string            `json:"impact_analysis,omitempty"`
	SessionDir    string             `json:"-"`
	sessionID     string             `json:"-"`
}

func NewState(sessionDir, sessionID string) *State {
	s := &State{
		Current:    StageRefine,
		Statuses:   make(map[Stage]StageStatus),
		SessionDir: sessionDir,
		sessionID:  sessionID,
	}
	for _, st := range AllStages {
		s.Statuses[st] = StatusPending
	}
	s.StageLabels = make(map[Stage]string)
	for k, v := range StageLabels {
		s.StageLabels[k] = v
	}
	s.StagePrompts = make(map[Stage]string)
	s.StageOrder = AllStages
	s.Statuses[StageRefine] = StatusInProgress
	return s
}

// NewEmptyState creates a workflow state with no stages selected.
func NewEmptyState(sessionDir, sessionID string) *State {
	return &State{
		Current:    "",
		Statuses:   make(map[Stage]StageStatus),
		StageLabels: make(map[Stage]string),
		StagePrompts: make(map[Stage]string),
		StageOrder:  nil,
		SessionDir: sessionDir,
		sessionID:  sessionID,
	}
}

func (s *State) Advance() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.StageOrder
	if len(order) == 0 {
		order = AllStages
	}
	s.Statuses[s.Current] = StatusCompleted
	for i, st := range order {
		if st == s.Current && i+1 < len(order) {
			s.Current = order[i+1]
			s.Statuses[s.Current] = StatusInProgress
			s.persist()
			return true
		}
	}
	s.persist()
	return false
}

func (s *State) Rollback() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.StageOrder
	if len(order) == 0 {
		order = AllStages
	}
	s.Statuses[s.Current] = StatusPending
	for i, st := range order {
		if st == s.Current && i-1 >= 0 {
			s.Current = order[i-1]
			s.Statuses[s.Current] = StatusInProgress
			s.persist()
			return true
		}
	}
	s.persist()
	return false
}

func (s *State) SetStage(stage Stage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	order := s.StageOrder
	if len(order) == 0 {
		order = AllStages
	}
	for _, st := range order {
		if st == stage {
			for _, st2 := range order {
				s.Statuses[st2] = StatusPending
			}
			s.Current = stage
			s.Statuses[stage] = StatusInProgress
			s.persist()
			return true
		}
	}
	return false
}

func (s *State) CurrentStage() Stage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Current
}

func (s *State) Snapshot() map[Stage]StageStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[Stage]StageStatus, len(s.Statuses))
	for k, v := range s.Statuses {
		out[k] = v
	}
	return out
}

func (s *State) StagePrompt() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	prompts := map[Stage]string{
		StageRefine:  "## Stage: refine - Clarify requirements, boundaries, and acceptance criteria with the user.",
		StageDevelop: "## Stage: develop - Implement the feature following project conventions. Write clean, maintainable code.",
		StageReview:  "## Stage: review - Review completed code for correctness, quality, performance, and security.",
		StageTest:    "## Stage: test - Write and run tests. Ensure coverage and no regressions.",
		StageCommit:  "## Stage: commit - Organize changes, write a commit message, and commit.",
	}
	if p, ok := prompts[s.Current]; ok {
		return p
	}
	return ""
}

// SetImpactAnalysis stores the impact analysis result for injection into agent prompt.
func (s *State) SetImpactAnalysis(result string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ImpactAnalysis = result
	s.persist()
}

// GetImpactAnalysis returns the stored impact analysis result and clears it.
func (s *State) GetImpactAnalysis() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.ImpactAnalysis
	s.ImpactAnalysis = ""
	s.persist()
	return r
}

func (s *State) persist() {
	if s.SessionDir == "" || s.sessionID == "" {
		return
	}
	data, err := json.Marshal(s)
	if err != nil {
		return
	}
	path := filepath.Join(s.SessionDir, s.sessionID+".workflow.json")
	os.WriteFile(path, data, 0644)
}

func LoadState(sessionDir, sessionID string) *State {
	path := filepath.Join(sessionDir, sessionID+".workflow.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return NewState(sessionDir, sessionID)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return NewState(sessionDir, sessionID)
	}
	s.SessionDir = sessionDir
	s.sessionID = sessionID
	return &s
}

func DetectStageComplete(output string) bool {
	return strings.Contains(output, "__STAGE_COMPLETE__")
}

func CompleteReason(output string) string {
	idx := strings.Index(output, "__STAGE_COMPLETE__:")
	if idx < 0 {
		return ""
	}
	rest := output[idx+20:]
	if len(rest) > 100 {
		rest = rest[:100]
	}
	return rest
}

var _ = fmt.Sprintf


// SelectTemplate replaces the workflow stages with those from a template.
func (s *State) SelectTemplate(stages []Stage, templateStages []TemplateStage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	newStatuses := make(map[Stage]StageStatus)
	for _, st := range stages {
		newStatuses[st] = StatusPending
	}
	// Store template stage labels
	newLabels := make(map[Stage]string)
	newPrompts := make(map[Stage]string)
	for _, ts := range templateStages {
		newLabels[Stage(ts.Name)] = ts.Label
		newPrompts[Stage(ts.Name)] = ts.Prompt
	}
	if len(stages) > 0 {
		s.Current = stages[0]
		newStatuses[stages[0]] = StatusInProgress
	} else {
		s.Current = ""
	}
	s.Statuses = newStatuses
	s.StageLabels = newLabels
	s.StagePrompts = newPrompts
	// Preserve insertion order
	order := make([]Stage, len(templateStages))
	for i, ts := range templateStages {
		order[i] = Stage(ts.Name)
	}
	s.StageOrder = order
	s.persist()
}

// StagePromptByTemplate returns the prompt for the current stage based on template stages.
func (s *State) GetStagePrompt(stage Stage) string {
	// Try to get the prompt from stored template stage data
	// For now, return the label-based prompt
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.StagePrompts == nil {
		return ""
	}
	return s.StagePrompts[stage]
}

func (s *State) SetSessionInfo(sessionDir, sessionID string) {
	if sessionDir == "" || sessionID == "" {
		return
	}
	s.SessionDir = sessionDir
	s.sessionID = sessionID
}

func (s *State) GetStageLabel(stage Stage) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.StageLabels == nil {
		return "", false
	}
	l, ok := s.StageLabels[stage]
	return l, ok
}

// FindStageByLabel does a reverse lookup: given a label or stage name string,
// returns the matching Stage. It first tries exact name match, then case-insensitive
// label match, then prefix match on label.
func (s *State) FindStageByLabel(input string) (Stage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Try exact match first
	for _, st := range s.StageOrder {
		if string(st) == input {
			return st, true
		}
	}
	// Try label match
	lower := strings.ToLower(input)
	for _, st := range s.StageOrder {
		if l, ok := s.StageLabels[st]; ok {
			if strings.ToLower(l) == lower || strings.HasPrefix(strings.ToLower(l), lower) {
				return st, true
			}
		}
		// Also match stage name prefix
		if strings.HasPrefix(string(st), lower) {
			return st, true
		}
	}
	return "", false
}

// OrderedStages returns stages in the order defined by the selected template.
func (s *State) OrderedStages() []Stage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Statuses) == 0 || len(s.StageOrder) == 0 {
		return nil
	}
	return s.StageOrder
}

func (s *State) StagePromptByTemplate(templateStages []TemplateStage) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ts := range templateStages {
		if Stage(ts.Name) == s.Current {
			return ts.Prompt
		}
	}
	return ""
}


//