package tracks

import (
	"fmt"
	"reflect"
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

type AnnotationMeta struct {
	Start, End Timestamp
	Type       string
	ID         ID
}

type Annotation struct {
	AnnotationMeta
	Data  any
	track *Track
}

func (a Annotation) Span() Span {
	return &filteredSpan{a.Start, a.End, a.track}
}

func (a *Annotation) UnmarshalCBOR(data []byte) error {
	type Annotation2 struct {
		AnnotationMeta
		Data cbor.RawMessage
	}
	var a2 Annotation2
	if err := cbor.Unmarshal(data, &a2); err != nil {
		return err
	}
	typ, ok := annotationTypes[a2.Type]
	if !ok {
		return fmt.Errorf("unknown annotation type %q", a2.Type)
	}
	value := reflect.New(typ)
	if err := cbor.Unmarshal(a2.Data, value.Interface()); err != nil {
		return err
	}
	a.AnnotationMeta = a2.AnnotationMeta
	a.Data = reflect.Indirect(value).Interface()
	return nil
}

type Session struct {
	ID     ID
	Start  time.Time
	Tracks []*Track
}

func NewSession() *Session {
	return &Session{
		ID:    newID(),
		Start: time.Now().UTC(),
	}
}

func (s *Session) NewTrack(format beep.Format) *Track {
	start := time.Now().UTC().Sub(s.Start)
	return s.NewTrackAt(Timestamp(start), format)
}

func (s *Session) NewTrackAt(start Timestamp, format beep.Format) *Track {
	t := &Track{
		ID:      newID(),
		Session: s,
		start:   start,
		audio:   beep.NewBuffer(format),
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
	ID      ID
	Session *Session
	start   Timestamp
	audio   *beep.Buffer
	// opus packets
	annotations []Annotation
}

var _ Span = (*Track)(nil)

func (t *Track) Annotate(typ string, data any) Annotation {
	return t.annotate(typ, t, data)
}

func (t *Track) annotate(typ string, span Span, data any) Annotation {
	a := Annotation{
		AnnotationMeta: AnnotationMeta{
			ID:    newID(),
			Start: span.Start(),
			End:   span.End(),
			Type:  typ,
		},
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

func (t *Track) Audio() beep.Streamer {
	return t.audio.Streamer(0, t.audio.Len())
}

func (t *Track) AudioFormat() beep.Format {
	return t.audio.Format()
}

func (t *Track) AddAudio(streamer beep.Streamer) {
	t.audio.Append(streamer)
}

func (t *Track) Span(from Timestamp, to Timestamp) Span {
	return &filteredSpan{from, to, t}
}

// Start implements Span.
func (t *Track) Start() Timestamp {
	return t.start
}

func (t *Track) End() Timestamp {
	if t.audio == nil {
		return t.start
	}
	dur := t.audio.Format().SampleRate.D(t.audio.Len())
	return t.start + Timestamp(dur)
}

func (t *Track) Track() *Track {
	return t
}

type trackMarshal struct {
	ID          ID
	Annotations []Annotation
	Start       Timestamp
	Format      beep.Format
}

func (t *Track) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(trackMarshal{
		ID:          t.ID,
		Annotations: t.annotations,
		Start:       t.start,
		Format:      t.audio.Format(),
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
	t.audio = beep.NewBuffer(tm.Format)
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

func (s *filteredSpan) Audio() beep.Streamer {
	startOffset := time.Duration(s.start - s.track.start)
	dur := time.Duration(s.end - s.start)
	from := s.track.audio.Format().SampleRate.N(startOffset)
	to := from + s.track.audio.Format().SampleRate.N(dur)
	return s.track.audio.Streamer(from, to)
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

var annotationTypes = map[string]reflect.Type{}

func RegisterAnnotation[T any](name string) {
	var t T
	annotationTypes[name] = reflect.TypeOf(t)
}
