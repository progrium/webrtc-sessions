package main

import (
	"context"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/speaker"
	"github.com/pion/webrtc/v3"
	"github.com/progrium/webrtc-sessions/local"
	"github.com/progrium/webrtc-sessions/trackstreamer"
	"github.com/progrium/webrtc-sessions/vad"
	"tractor.dev/toolkit-go/engine"
)

func main() {
	engine.Run(Main{})
}

type Main struct{}

func fatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func (m *Main) Serve(ctx context.Context) {
	host := os.Getenv("SFU_SERVER")
	if host == "" {
		host = "ws://localhost:8088"
	}

	hostURL, err := url.JoinPath(host, "session")
	fatal(err)
	peer, err := local.NewPeer(hostURL)
	fatal(err)

	format := beep.Format{
		SampleRate:  beep.SampleRate(16000),
		NumChannels: 1,
	}
	fatal(speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10)))

	peer.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("got track %s %s", track.ID(), track.Kind())
		if track.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		s, err := trackstreamer.New(track, format)
		fatal(err)
		a, b := beep.Dup(s)
		speaker.Play(a)

		detector := vad.New(vad.Config{
			SampleRate:   format.SampleRate.N(time.Second),
			SampleWindow: 24 * time.Second,
		})
		b = effects.Mono(b) // should already be mono, but we can make sure
		// b = &effects.Volume{Streamer: b, Base: 2, Volume: 3}
		var totSamples int
		for {
			samples := make([][2]float64, 1000)
			n, ok := b.Stream(samples)
			if !ok {
				fatal(b.Err())
				break
			}
			pcm := make([]float32, n)
			for i, s := range samples[:n] {
				pcm[i] = float32(s[0])
			}
			totSamples += n
			out := detector.Push(&vad.CapturedSample{
				PCM:          pcm,
				EndTimestamp: uint32(format.SampleRate.D(totSamples) / time.Millisecond),
			})
			if out != nil {
				log.Printf("got output chunk of %d samples", len(out.PCM))
			}
		}
	})

	peer.HandleSignals()
}
