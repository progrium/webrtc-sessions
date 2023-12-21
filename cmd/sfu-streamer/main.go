package main

import (
	"context"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/pion/webrtc/v3"
	"github.com/progrium/webrtc-sessions/local"
	"github.com/progrium/webrtc-sessions/trackstreamer"
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
		speaker.Play(s)
	})

	peer.HandleSignals()
}
