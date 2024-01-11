package tracks

import (
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/generators"
	"github.com/stretchr/testify/require"
	"gotest.tools/assert"
)

var eqopts = cmp.Options{
	cmp.AllowUnexported(Session{}, Track{}, beep.Buffer{}),
	cmpopts.IgnoreFields(Annotation{}, "track"),
}

func init() {
	RegisterAnnotation[string]("text")
}

func TestTrack(t *testing.T) {
	rate := beep.SampleRate(48000)
	track := &Track{
		start: 0,
		audio: beep.NewBuffer(beep.Format{
			SampleRate:  rate,
			NumChannels: 2,
			Precision:   2,
		}),
	}
	track.AddAudio(generators.Silence(rate.N(10 * time.Millisecond)))
	assert.DeepEqual(t, []Annotation(nil), track.Annotations("text"))

	track.Annotate("text", "foo-one")
	types := track.AnnotationTypes()
	assert.DeepEqual(t, []string{"text"}, types)

	assert.DeepEqual(t,
		[]Annotation{
			{AnnotationMeta: AnnotationMeta{Start: 0, End: Timestamp(10 * time.Millisecond), Type: "text"}, Data: "foo-one"},
		},
		track.Annotations("text"),
		eqopts, cmpopts.IgnoreFields(Track{}, "ID"), cmpopts.IgnoreFields(Annotation{}, "ID"),
	)

	track.Span(Timestamp(5*time.Millisecond), Timestamp(10*time.Millisecond)).Annotate("text", "foo-two")
	assert.DeepEqual(t,
		[]Annotation{
			{AnnotationMeta: AnnotationMeta{Start: 0, End: Timestamp(10 * time.Millisecond), Type: "text"}, Data: "foo-one"},
			{AnnotationMeta: AnnotationMeta{Start: Timestamp(5 * time.Millisecond), End: Timestamp(10 * time.Millisecond), Type: "text"}, Data: "foo-two"},
		},
		track.Annotations("text"),
		eqopts, cmpopts.IgnoreFields(Track{}, "ID"), cmpopts.IgnoreFields(Annotation{}, "ID"),
	)
}

func assertCBORRoundTrip[T any](t *testing.T, in T, opts ...cmp.Option) {
	t.Helper()
	data, err := cbor.Marshal(in)
	require.NoError(t, err)
	var out T
	err = cbor.Unmarshal(data, &out)
	require.NoError(t, err)
	assert.DeepEqual(t, in, out, opts...)
}

func TestSerializeAnnotationTypes(t *testing.T) {
	a := Annotation{
		AnnotationMeta: AnnotationMeta{
			Start: 0, End: Timestamp(10 * time.Millisecond),
			Type: "text",
		},
		Data: "foo-a",
	}
	assertCBORRoundTrip(t, a, eqopts)

	type MyType struct {
		Foo string
	}
	RegisterAnnotation[MyType]("my-type")
	b := Annotation{
		AnnotationMeta: AnnotationMeta{
			Start: 0, End: Timestamp(10 * time.Millisecond),
			Type: "my-type",
		},
		Data: MyType{Foo: "foo-b"},
	}
	assertCBORRoundTrip(t, b, eqopts)
}

func TestSerializeTrack(t *testing.T) {
	track := &Track{
		start: 0,
		audio: beep.NewBuffer(beep.Format{
			SampleRate:  48000,
			NumChannels: 2,
			Precision:   2,
		}),
	}
	track.Annotate("text", "foo-one")
	track.Span(Timestamp(5*time.Millisecond), Timestamp(10*time.Millisecond)).Annotate("text", "foo-two")

	out, err := cbor.Marshal(track)
	require.NoError(t, err)

	var track2 Track
	require.NoError(t, cbor.Unmarshal(out, &track2))

	assert.DeepEqual(t, track, &track2, eqopts)
}

func TestSerializeSession(t *testing.T) {
	session := &Session{}
	track := session.NewTrackAt(0, beep.Format{
		SampleRate:  beep.SampleRate(48000),
		NumChannels: 2,
		Precision:   2,
	})
	track.Annotate("text", "foo-one")
	track.Span(Timestamp(5*time.Millisecond), Timestamp(10*time.Millisecond)).Annotate("text", "foo-two")

	out, err := cbor.Marshal(session)
	require.NoError(t, err)

	var session2 Session
	require.NoError(t, cbor.Unmarshal(out, &session2))

	assert.DeepEqual(t, session, &session2, eqopts)
}

func audioGenerator(t *testing.T) beep.Streamer {
	t.Helper()
	gen, err := generators.SineTone(beep.SampleRate(1000), 300)
	require.NoError(t, err)
	return gen
}

func assertEqualAudio(t *testing.T, format beep.Format, a, b beep.Streamer) {
	t.Helper()
	buf1 := beep.NewBuffer(format)
	buf1.Append(a)
	buf2 := beep.NewBuffer(format)
	buf2.Append(b)
	assert.DeepEqual(t, buf1, buf2, cmp.AllowUnexported(beep.Buffer{}))
}

func discardSamples(t *testing.T, n int, s beep.Streamer) {
	t.Helper()
	var samples [512][2]float64
	for n > 0 {
		m, ok := s.Stream(samples[:n])
		require.True(t, ok)
		n -= m
	}
}

func TestAudio(t *testing.T) {
	format := beep.Format{
		SampleRate:  beep.SampleRate(1000),
		NumChannels: 1,
		Precision:   2,
	}
	session := &Session{}
	track := session.NewTrackAt(0, format)
	gen := audioGenerator(t)

	assert.Equal(t, Timestamp(0), track.End(), "End should start at 0")

	track.AddAudio(beep.Take(format.SampleRate.N(1*time.Second), gen))
	assert.Equal(t, Timestamp(1*time.Second), track.End(), "adding one second of audio at the sample rate should increase the End by 1s")

	track.AddAudio(beep.Take(format.SampleRate.N(1*time.Second), gen))
	assert.Equal(t, Timestamp(2*time.Second), track.End(), "End should now be 2s")

	buf := beep.NewBuffer(format)
	buf.Append(track.Audio())
	assert.Equal(t, format.SampleRate.N(2*time.Second), buf.Len(), "track.Audio() should contain 2s of data")

	gen2s := beep.Take(format.SampleRate.N(2*time.Second), audioGenerator(t))
	assertEqualAudio(t, format, gen2s, track.Audio())

	// pick a start that won't align with the sine wave to make sure we're getting
	// the right segment
	midStart := 101 * time.Millisecond
	midEnd := 1500 * time.Millisecond
	middle := track.Span(Timestamp(midStart), Timestamp(midEnd))
	genMid := audioGenerator(t)
	discardSamples(t, format.SampleRate.N(midStart), genMid)
	genMid = beep.Take(format.SampleRate.N(midEnd-midStart), genMid)
	assertEqualAudio(t, format, genMid, middle.Audio())
}
