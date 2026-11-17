package domain

import "encoding/json"

// historyElement is the shared contract every element of an append-only
// revision history must satisfy. Elements are stored by value while keeping
// the revision itself service-managed: callers never assign it directly.
// withRevision is functional (it returns a numbered copy) because value
// receiver methods on a generic type parameter mutate only a boxed copy.
// Concrete element types (Sheet, Recipe) own their own validity rules and
// freeze behaviour, keeping domain facts out of the generic revision
// mechanism.
type historyElement[T any] interface {
	Sheet | Recipe
	withRevision(int) T
	freezeCopy() *T
}

// History is the shared append-only revision mechanism.
//
// Boundary decision: the history owns only the generic facts (monotonic
// revisions, optimistic concurrency, element storage and the legacy JSON
// shape). It never knows what makes a Sheet or Recipe valid; the validate
// callback supplied on append belongs to the owning domain.
//
// Storage is copy-on-write rather than a full deep copy per read: a candidate
// is frozen exactly once when it is committed (freezeCopy), and reads hand
// back the committed element without re-copying. Elements handed back by At,
// Latest and Range are therefore immutable facts and must not be mutated by
// callers; edits always flow through Append, which starts from a fresh request
// value.
type History[T historyElement[T]] struct {
	items []T
}

// NewHistory starts an empty revision history.
func NewHistory[T historyElement[T]]() *History[T] { return &History[T]{} }

// Len returns the number of committed revisions (the current expected
// revision for the next append).
func (h *History[T]) Len() int {
	if h == nil {
		return 0
	}
	return len(h.items)
}

// At returns the revision at the given 1-based position. The returned value is
// a committed, immutable domain fact and must not be mutated.
func (h *History[T]) At(revision int) (T, bool) {
	var zero T
	if h == nil || revision < 1 || revision > len(h.items) {
		return zero, false
	}
	return h.items[revision-1], true
}

// Latest returns the most recently committed revision. The returned value is
// a committed, immutable domain fact and must not be mutated.
func (h *History[T]) Latest() (T, bool) {
	var zero T
	if h == nil || len(h.items) == 0 {
		return zero, false
	}
	return h.items[len(h.items)-1], true
}

// Range visits committed revisions in order, starting at revision 1. Returning
// true stops iteration. The visited values are immutable domain facts.
func (h *History[T]) Range(visit func(revision int, value T) (stop bool)) {
	if h == nil {
		return
	}
	for i, v := range h.items {
		if visit(i+1, v) {
			return
		}
	}
}

// Append commits candidate as the next revision.
//
// expected is the If-Match style optimistic revision the writer based its edit
// on; it must equal the current history length or the append is rejected with
// a conflict and no state changes. validate runs against the not-yet-numbered
// candidate so each domain keeps its own validity judgement; the generic
// mechanism only enforces the revision protocol around it.
//
// The committed value is a single freeze copy of the candidate, so the
// caller's request object can never alias mutable state that survives the
// transaction. The returned value is the immutable committed fact.
func (h *History[T]) Append(expected int, candidate T, validate func(T) error) (T, error) {
	var zero T
	if h == nil {
		return zero, Invalid("revision history is not initialized")
	}
	if len(h.items) != expected {
		return zero, Conflict("revision changed")
	}
	if err := validate(candidate); err != nil {
		return zero, err
	}
	candidate = candidate.withRevision(len(h.items) + 1)
	committed := *candidate.freezeCopy()
	h.items = append(h.items, committed)
	return committed, nil
}

// MarshalJSON preserves the legacy on-disk shape: a history serializes as a
// bare JSON array, exactly like the former map[string][]T collections.
func (h *History[T]) MarshalJSON() ([]byte, error) {
	if h == nil || len(h.items) == 0 {
		return []byte("[]"), nil
	}
	return json.Marshal(h.items)
}

// UnmarshalJSON reads the legacy bare-array layout back into the shared
// history type, so old state.json files open without migration.
func (h *History[T]) UnmarshalJSON(data []byte) error {
	var items []T
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	h.items = items
	return nil
}
