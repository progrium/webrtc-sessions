package main

import (
	"context"
	"log"

	"github.com/progrium/webrtc-sessions/local"
	"tractor.dev/toolkit-go/engine"
)

func main() {
	engine.Run(Main{})
}

type Main struct{}

func (m *Main) Serve(ctx context.Context) {
	peer, err := local.NewPeer("ws://localhost:8088/session")
	if err != nil {
		log.Fatal(err)
	}
	peer.HandleSignals()
}
