package domain

import "time"

type Media struct {
	DurationMS int64  `json:"duration_ms"`
	Video      bool   `json:"video"`
	Audio      bool   `json:"audio"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	VideoCodec string `json:"video_codec"`
	AudioCodec string `json:"audio_codec"`
}
type Source struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Digest    string    `json:"digest"`
	Bytes     int64     `json:"bytes"`
	Media     Media     `json:"media"`
	CreatedAt time.Time `json:"created_at"`
}
type Span struct {
	SourceID string  `json:"source_id"`
	InMS     int64   `json:"in_ms"`
	OutMS    int64   `json:"out_ms"`
	GainDB   float64 `json:"gain_db"`
}
type Cue struct {
	SourceID string `json:"source_id"`
	StartMS  int64  `json:"start_ms"`
	EndMS    int64  `json:"end_ms"`
	Text     string `json:"text"`
}
type Sheet struct {
	ID        string    `json:"id"`
	Revision  int       `json:"revision"`
	Name      string    `json:"name"`
	Spans     []Span    `json:"spans"`
	Cues      []Cue     `json:"cues"`
	CreatedAt time.Time `json:"created_at"`
}

// withRevision is service-managed revision assignment; clients never send a
// trusted revision and never call this directly. It is a value-receiver
// functional method so it also works when Sheet is a generic type parameter.
func (s Sheet) withRevision(revision int) Sheet {
	s.Revision = revision
	return s
}

// freezeCopy returns an element whose slice backing arrays cannot alias the
// caller's request object. Committed sheets are immutable facts, so this copy
// happens once at the history boundary rather than on every read.
func (s Sheet) freezeCopy() *Sheet {
	frozen := s
	if s.Spans != nil {
		frozen.Spans = append([]Span(nil), s.Spans...)
	}
	if s.Cues != nil {
		frozen.Cues = append([]Cue(nil), s.Cues...)
	}
	return &frozen
}

type Recipe struct {
	ID         string  `json:"id"`
	Revision   int     `json:"revision"`
	Name       string  `json:"name"`
	Format     string  `json:"format"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	FPS        int     `json:"fps"`
	SampleRate int     `json:"sample_rate"`
	Normalize  bool    `json:"normalize"`
	TargetLUFS float64 `json:"target_lufs"`
}

// withRevision is service-managed revision assignment for recipes. Value
// receiver so the functional form also applies through a generic parameter.
func (r Recipe) withRevision(revision int) Recipe {
	r.Revision = revision
	return r
}

// freezeCopy returns the committed recipe. A recipe holds only scalar fields,
// so the value itself is the immutable fact; the method exists to satisfy the
// shared history contract alongside reference-bearing types such as Sheet.
func (r Recipe) freezeCopy() *Recipe {
	frozen := r
	return &frozen
}

type Placement struct {
	SourceID    string  `json:"source_id"`
	SourceInMS  int64   `json:"source_in_ms"`
	SourceOutMS int64   `json:"source_out_ms"`
	OutputInMS  int64   `json:"output_in_ms"`
	OutputOutMS int64   `json:"output_out_ms"`
	GainDB      float64 `json:"gain_db"`
}
type Plan struct {
	DurationMS int64       `json:"duration_ms"`
	Placements []Placement `json:"placements"`
	Recipe     Recipe      `json:"recipe"`
	VTT        string      `json:"vtt"`
}
