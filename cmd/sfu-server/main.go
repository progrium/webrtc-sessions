package main

import (
	"github.com/progrium/webrtc-sessions/sfu"
	"tractor.dev/toolkit-go/engine"
)

func main() {
	engine.Run(sfu.Service{})
}
