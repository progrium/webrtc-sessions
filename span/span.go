package span

import (
	"sort"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gopxl/beep"
	"github.com/rs/xid"
)

type Timestamp time.Duration // relative to stream start
type ID string

type Span interface {
	Track() *Track
	Span(from, to Timestamp) Span
	Start() Timestamp
	End() Timestamp
	Audio() beep.Streamer
	// I guess this returns each annotation found in the span
	// -- list annotations
	AnnotationTypes() []string
	Annotations(typ string) []Annotation
	// But this would overwrite all the annotations for the span of that type?
	Annotate(typ string, data any) Annotation
}

type Annotator interface {
	Annotated(Annotation)
}

func newID() ID {
	// FIXME
	return ID(xid.New().String())
}

type Annotation struct {
	Start, End Timestamp
	Type       string
	ID         ID
	Data       any
	track      *Track
}

func (a Annotation) Span() Span {
	return &filteredSpan{a.Start, a.End, a.track}
}

type Session struct {
	ID     ID
	Start  time.Time
	Tracks []*Track
}

func (s *Session) NewTrack(start Timestamp) *Track {
	t := &Track{
		ID:      newID(),
		Session: s,
		start:   start,
	}
	s.Tracks = append(s.Tracks, t)
	return t
}

func (s *Session) UnmarshalCBOR(data []byte) error {
	type Session2 Session
	var s2 Session2
	if err := cbor.Unmarshal(data, &s2); err != nil {
		return err
	}
	*s = Session(s2)
	for _, t := range s.Tracks {
		t.Session = s
	}
	return nil
}

type Track struct {
	ID         ID
	Session    *Session
	start, end Timestamp
	format     beep.Format // ?
	// End should be derived from the last audio sample?
	samples [][2]float32 // or beep buffer?
	// opus packets
	annotations []Annotation
}

var _ Span = (*Track)(nil)

func (t *Track) Annotate(typ string, data any) Annotation {
	return t.annotate(typ, t, data)
}

func (t *Track) annotate(typ string, span Span, data any) Annotation {
	a := Annotation{
		ID:    newID(),
		Start: span.Start(),
		End:   span.End(),
		Type:  typ,
		Data:  data,
		track: t,
	}
	t.annotations = append(t.annotations, a)
	return a
}

func (t *Track) AnnotationTypes() []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range t.annotations {
		if seen[a.Type] {
			continue
		}
		seen[a.Type] = true
		out = append(out, a.Type)
	}
	sort.Strings(out)
	return out
}

func (t *Track) Annotations(typ string) []Annotation {
	var out []Annotation
	for _, a := range t.annotations {
		if a.Type == typ {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Start < out[j].Start {
			return true
		}
		if out[i].Start > out[j].Start {
			return false
		}
		return out[i].End < out[j].End
	})
	return out
}

func (*Track) Audio() beep.Streamer {
	panic("unimplemented")
}

func (t *Track) Span(from Timestamp, to Timestamp) Span {
	return &filteredSpan{from, to, t}
}

// Start implements Span.
func (t *Track) Start() Timestamp {
	return t.start
}

func (t *Track) End() Timestamp {
	return t.end
}

func (t *Track) Track() *Track {
	return t
}

func (t *Track) AddAudio(samples [][2]float32) {
	panic("unimplemented")
}

type trackMarshal struct {
	ID          ID
	Annotations []Annotation
	Start, End  Timestamp
}

func (t *Track) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(trackMarshal{
		ID:          t.ID,
		Annotations: t.annotations,
		Start:       t.start,
		End:         t.end,
	})
}

func (t *Track) UnmarshalCBOR(data []byte) error {
	var tm trackMarshal
	if err := cbor.Unmarshal(data, &tm); err != nil {
		return err
	}
	t.ID = tm.ID
	t.annotations = tm.Annotations
	for _, a := range t.annotations {
		a.track = t
	}
	t.start = tm.Start
	t.end = tm.End
	return nil
}

type filteredSpan struct {
	start, end Timestamp
	track      *Track
}

var _ Span = (*filteredSpan)(nil)

func (s *filteredSpan) Annotate(typ string, data any) Annotation {
	return s.Track().annotate(typ, s, data)
}

func (s *filteredSpan) AnnotationTypes() []string {
	// TODO should it return all types for the Track, or only ones found within this span?
	return s.Track().AnnotationTypes()
}

func (s *filteredSpan) Annotations(typ string) []Annotation {
	var out []Annotation
	for _, a := range s.track.Annotations(typ) {
		if a.End < s.start || a.Start > s.end {
			continue
		}
		// TODO clamp the start/end times of this annotation to the span start/end?
		out = append(out, a)
	}
	return out
}

func (*filteredSpan) Audio() beep.Streamer {
	// TODO limit audio stream to the start/end times
	panic("unimplemented")
}

func (s *filteredSpan) End() Timestamp {
	return s.end
}

func (s *filteredSpan) Span(from Timestamp, to Timestamp) Span {
	return &filteredSpan{from, to, s.track}
}

func (s *filteredSpan) Start() Timestamp {
	return s.start
}

func (s *filteredSpan) Track() *Track {
	return s.track
}

func RegisterAnnotation[T any](typ string) {}
