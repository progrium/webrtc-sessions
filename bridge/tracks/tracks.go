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
	EventTypes() []string
	Events(typ string) []Event
	RecordEvent(typ string, data any) Event
}

type Handler interface {
	HandleEvent(Event)
}

func newID() ID {
	// FIXME
	return ID(xid.New().String())
}

type EventMeta struct {
	Start, End Timestamp
	Type       string
	ID         ID
}

type Event struct {
	EventMeta
	Data  any
	track *Track
}

func (e Event) Span() Span {
	return &filteredSpan{e.Start, e.End, e.track}
}

func (e *Event) UnmarshalCBOR(data []byte) error {
	type EventRawData struct {
		EventMeta
		Data cbor.RawMessage
	}
	var eraw EventRawData
	if err := cbor.Unmarshal(data, &eraw); err != nil {
		return err
	}
	typ, ok := eventTypes[eraw.Type]
	if !ok {
		return fmt.Errorf("unknown event type %q", eraw.Type)
	}
	value := reflect.New(typ)
	if err := cbor.Unmarshal(eraw.Data, value.Interface()); err != nil {
		return err
	}
	e.EventMeta = eraw.EventMeta
	e.Data = reflect.Indirect(value).Interface()
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
		audio:   newContinuousBuffer(format),
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
	audio   *continuousBuffer
	// opus packets
	events []Event
}

var _ Span = (*Track)(nil)

func (t *Track) RecordEvent(typ string, data any) Event {
	return t.record(typ, t, data)
}

func (t *Track) record(typ string, span Span, data any) Event {
	// FIXME add lock
	a := Event{
		EventMeta: EventMeta{
			ID:    newID(),
			Start: span.Start(),
			End:   span.End(),
			Type:  typ,
		},
		Data:  data,
		track: t,
	}
	t.events = append(t.events, a)
	return a
}

func (t *Track) UpdateEvent(evt Event) bool {
	// this is a copy so it won't affect the caller, but make sure it's pointing
	// to this track
	evt.track = t
	for i, a := range t.events {
		if a.ID == evt.ID {
			t.events[i] = evt
			return true
		}
	}
	return false
}

func (t *Track) EventTypes() []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range t.events {
		if seen[a.Type] {
			continue
		}
		seen[a.Type] = true
		out = append(out, a.Type)
	}
	sort.Strings(out)
	return out
}

func (t *Track) Events(typ string) []Event {
	var out []Event
	for _, a := range t.events {
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
	return t.audio.StreamerFrom(0)
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
	ID     ID
	Events []Event
	Start  Timestamp
	Format beep.Format
}

func (t *Track) MarshalCBOR() ([]byte, error) {
	return cbor.Marshal(trackMarshal{
		ID:     t.ID,
		Events: t.events,
		Start:  t.start,
		Format: t.audio.Format(),
	})
}

func (t *Track) UnmarshalCBOR(data []byte) error {
	var tm trackMarshal
	if err := cbor.Unmarshal(data, &tm); err != nil {
		return err
	}
	t.ID = tm.ID
	t.events = tm.Events

	for _, a := range t.events {
		a.track = t
	}
	t.start = tm.Start
	t.audio = newContinuousBuffer(tm.Format)
	return nil
}

type filteredSpan struct {
	start, end Timestamp
	track      *Track
}

var _ Span = (*filteredSpan)(nil)

func (s *filteredSpan) RecordEvent(typ string, data any) Event {
	return s.Track().record(typ, s, data)
}

func (s *filteredSpan) EventTypes() []string {
	// TODO should it return all types for the Track, or only ones found within this span?
	return s.Track().EventTypes()
}

func (s *filteredSpan) Events(typ string) []Event {
	var out []Event
	for _, a := range s.track.Events(typ) {
		if a.End < s.start || a.Start > s.end {
			continue
		}
		out = append(out, a)
	}
	return out
}

func (s *filteredSpan) Audio() beep.Streamer {
	startOffset := time.Duration(s.start - s.track.start)
	dur := time.Duration(s.end - s.start)
	from := s.track.audio.Format().SampleRate.N(startOffset)
	samples := s.track.audio.Format().SampleRate.N(dur)
	return beep.Take(samples, s.track.audio.StreamerFrom(from))
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

var eventTypes = map[string]reflect.Type{}

func RegisterEvent[T any](name string) {
	// TODO(Go 1.22) can use reflect.TypeFor[T]()
	eventTypes[name] = reflect.TypeOf((*T)(nil)).Elem()
}
