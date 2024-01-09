package tracks

import (
	"time"

	"github.com/gopxl/beep"
	"github.com/rs/xid"
)

type Timestamp time.Duration // relative to stream start
type ID string

func newID() ID {
	// FIXME
	return ID(xid.New().String())
}

type Annotation struct {
	Start, End Timestamp
	Type       string
	ID         ID
	Data       any
}

func (a *Annotation) Span() Span {
	return nil // TODO
}

type Session struct {
	ID     ID
	Start  time.Time
	Tracks []*Track
}

type Track struct {
	ID      ID
	session *Session
	start   Timestamp
	Format  beep.Format // ?
	// End should be derived from the last audio sample?
	samples [][2]float32 // or beep buffer?
	// opus packets
	annotations []Annotation
}

func (t *Track) AddAudio(samples [][2]float32) {

}

func RegisterAnnotation[T any](typ string) {}

// func RegisterAnnotator(typ string, fn func(Annotation)) {
// 	// FIXME
// }

type Annotator interface {
	Annotated(Annotation)
}

// var _ Span = (*Track)(nil)

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
