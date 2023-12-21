package trackstreamer

import (
	"io"
	"time"

	"github.com/gopxl/beep"
	"gopkg.in/hraban/opus.v2"

	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
)

const (
	decodeBufDuration = 60 * time.Millisecond
)

type sampleDecoder interface {
	DecodeFloat32(data []byte, buf []float32) (int, error)
}

type TrackStreamer struct {
	track        *webrtc.TrackRemote
	format       beep.Format
	dec          sampleDecoder
	decodeBuf    []float32
	pcm          []float32
	sampleBuffer *samplebuilder.SampleBuilder
}

func New(track *webrtc.TrackRemote, format beep.Format) (beep.Streamer, error) {
	dec, err := opus.NewDecoder(format.SampleRate.N(time.Second), format.NumChannels)
	if err != nil {
		return nil, err
	}
	return &TrackStreamer{
		format:       format,
		decodeBuf:    make([]float32, format.NumChannels*format.SampleRate.N(decodeBufDuration)),
		dec:          dec,
		track:        track,
		sampleBuffer: samplebuilder.New(20, &codecs.OpusPacket{}, uint32(format.SampleRate.N(time.Second))),
	}, nil
}

var _ beep.Streamer = (*TrackStreamer)(nil)

func (*TrackStreamer) Err() error {
	return nil
}

func (t *TrackStreamer) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		samples[i], ok = t.nextPCM()
		if !ok {
			return i, false
		}
	}
	return len(samples), true
}

func (t *TrackStreamer) decodeNextPacket(buf []float32) (int, error) {
	s := t.sampleBuffer.Pop()
	for s == nil {
		pkt, _, err := t.track.ReadRTP()
		if err != nil {
			return 0, err
		}
		t.sampleBuffer.Push(pkt)
		s = t.sampleBuffer.Pop()
	}
	return t.dec.DecodeFloat32(s.Data, buf)
}

func (t *TrackStreamer) nextPCM() (sample [2]float64, ok bool) {
	for len(t.pcm) == 0 {
		n, err := t.decodeNextPacket(t.decodeBuf)
		if err != nil {
			if err == io.EOF {
				return [2]float64{}, false
			}
			continue // if we have a bad packet, try to get another
		}
		t.pcm = t.decodeBuf[:n*t.format.NumChannels]
	}
	left := float64(t.pcm[0])
	right := left
	if t.format.NumChannels > 1 {
		right = float64(t.pcm[1])
	}
	t.pcm = t.pcm[t.format.NumChannels:]
	return [2]float64{left, right}, true
}
