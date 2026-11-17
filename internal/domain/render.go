package domain

import "time"

type Attempt struct {
	Number     int        `json:"number"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Outcome    string     `json:"outcome"`
	Detail     string     `json:"detail,omitempty"`
}
type Inspection struct {
	Media           Media  `json:"media"`
	Digest          string `json:"digest"`
	Bytes           int64  `json:"bytes"`
	DurationDeltaMS int64  `json:"duration_delta_ms"`
}
type Artifact struct {
	Filename    string     `json:"filename"`
	CaptionFile string     `json:"caption_file"`
	Inspection  Inspection `json:"inspection"`
}
type Render struct {
	ID          string            `json:"id"`
	Key         string            `json:"key"`
	RequestHash string            `json:"request_hash"`
	Sheet       Sheet             `json:"sheet"`
	Recipe      Recipe            `json:"recipe"`
	Sources     map[string]Source `json:"sources"`
	Plan        Plan              `json:"plan"`
	State       string            `json:"state"`
	Attempts    []Attempt         `json:"attempts"`
	Artifact    *Artifact         `json:"artifact,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}
type Event struct {
	Sequence uint64    `json:"sequence"`
	Subject  string    `json:"subject"`
	Kind     string    `json:"kind"`
	Detail   string    `json:"detail"`
	At       time.Time `json:"at"`
}
type State struct {
	Schema   int               `json:"schema"`
	Sequence uint64            `json:"sequence"`
	Sources  map[string]Source `json:"sources"`
	// Sheets and Recipes keep their legacy JSON keys and bare-array values
	// (see History.MarshalJSON); only the in-memory boundary changes, moving
	// revision protocol and immutability into the shared History type while
	// sheet/recipe validity remains domain-specific.
	Sheets  map[string]*History[Sheet]  `json:"sheets"`
	Recipes map[string]*History[Recipe] `json:"recipes"`
	Renders map[string]Render           `json:"renders"`
	Events  []Event                     `json:"events"`
}

func Empty() State {
	return State{Schema: 1, Sources: map[string]Source{}, Sheets: map[string]*History[Sheet]{}, Recipes: map[string]*History[Recipe]{}, Renders: map[string]Render{}, Events: []Event{}}
}

// SheetHistory returns the revision history of a sheet, or nil if the sheet
// has never been created. Nil-safe so callers do not special-case missing keys.
func (s *State) SheetHistory(id string) *History[Sheet] { return s.Sheets[id] }

// RecipeHistory returns the revision history of a recipe, or nil if the recipe
// has never been created.
func (s *State) RecipeHistory(id string) *History[Recipe] { return s.Recipes[id] }
func (s *State) Emit(id, kind, detail string) {
	s.Sequence++
	s.Events = append(s.Events, Event{Sequence: s.Sequence, Subject: id, Kind: kind, Detail: detail, At: time.Now().UTC()})
}
func Terminal(state string) bool {
	return state == "succeeded" || state == "failed" || state == "cancelled"
}
