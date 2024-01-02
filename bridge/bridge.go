package bridge

import (
	"github.com/gopxl/beep"
)

type Span struct {
	Text    string
	Start   int
	End     int
	Speaker string
	Prob    float64
}

type Message struct {
	From string
	Text string
}

type AudioFrame struct {
	Timestamp int
	Audio     *beep.Buffer
	WordSpans []Span
	Ident     string
	Diarized  bool
}

func (f *AudioFrame) WordsStart() int {
	return f.WordSpans[0].Start
}

func (f *AudioFrame) WordsEnd() int {
	return f.WordSpans[len(f.WordSpans)-1].End
}
