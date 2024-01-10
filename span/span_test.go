package span

import (
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	"gotest.tools/assert"
)

func TestTrack(t *testing.T) {
	track := &Track{
		start: 0,
		end:   Timestamp(10 * time.Millisecond),
	}
	assert.DeepEqual(t, []Annotation(nil), track.Annotations("foo"))

	track.Annotate("foo", "foo-one")
	types := track.AnnotationTypes()
	assert.DeepEqual(t, []string{"foo"}, types)

	assert.DeepEqual(t, []Annotation{
		{Start: 0, End: Timestamp(10 * time.Millisecond), Type: "foo", Data: "foo-one"},
	}, track.Annotations("foo"), cmpopts.IgnoreFields(Annotation{}, "ID", "track"))

	track.Span(Timestamp(5*time.Millisecond), Timestamp(10*time.Millisecond)).Annotate("foo", "foo-two")
	assert.DeepEqual(t, []Annotation{
		{Start: 0, End: Timestamp(10 * time.Millisecond), Type: "foo", Data: "foo-one"},
		{Start: Timestamp(5 * time.Millisecond), End: Timestamp(10 * time.Millisecond), Type: "foo", Data: "foo-two"},
	}, track.Annotations("foo"), cmpopts.IgnoreFields(Annotation{}, "ID", "track"))
}

func TestSerializeTrack(t *testing.T) {
	track := &Track{
		start: 0,
		end:   Timestamp(10 * time.Millisecond),
	}
	track.Annotate("foo", "foo-one")
	track.Span(Timestamp(5*time.Millisecond), Timestamp(10*time.Millisecond)).Annotate("foo", "foo-two")

	// track2 := &Track{}
	// track2.Unmarshal(track.Marshal())
	// assert.DeepEqual(t, track, track2)

	out, err := cbor.Marshal(track)
	require.NoError(t, err)

	var track2 Track
	require.NoError(t, cbor.Unmarshal(out, &track2))

	assert.DeepEqual(t, track, &track2, cmp.AllowUnexported(Track{}), cmpopts.IgnoreFields(Annotation{}, "track"))
}

func TestSerializeSession(t *testing.T) {
	session := &Session{}
	track := session.NewTrack(0)
	track.Annotate("foo", "foo-one")
	track.Span(Timestamp(5*time.Millisecond), Timestamp(10*time.Millisecond)).Annotate("foo", "foo-two")

	out, err := cbor.Marshal(session)
	require.NoError(t, err)

	var session2 Session
	require.NoError(t, cbor.Unmarshal(out, &session2))

	assert.DeepEqual(t, session, &session2, cmp.AllowUnexported(Session{}, Track{}), cmpopts.IgnoreFields(Annotation{}, "track"))
}
