package span

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp/cmpopts"
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
