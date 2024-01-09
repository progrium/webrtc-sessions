package main

import (
	"log"
	"os"

	"github.com/fxamacker/cbor/v2"
	"github.com/gopxl/beep"
	"github.com/progrium/webrtc-sessions/bridge"
	"tractor.dev/toolkit-go/engine"
	"tractor.dev/toolkit-go/engine/cli"
)

func main() {
	engine.Run(Main{})
}

type Main struct {
	frames []*bridge.AudioFrame
	format beep.Format
}

func (m *Main) InitializeCLI(root *cli.Command) {
	root.Run = func(ctx *cli.Context, args []string) {
		m.format = beep.Format{
			SampleRate:  beep.SampleRate(16000),
			NumChannels: 1,
			Precision:   4,
		}

		b, err := os.ReadFile(args[0])
		if err != nil {
			log.Fatal(err)
		}
		if err := cbor.Unmarshal(b, &m.frames); err != nil {
			log.Fatal(err)
		}
		//buf := beep.NewBuffer(m.format)
		for _, frame := range m.frames {
			frame.Audio.
				fmt.Println(frame.Audio.Len())
			//buf.Append(frame.Audio.Streamer(0, frame.Audio.Len()))
		}
		//speaker.Play(buf.Streamer(0, buf.Len()))
	}
}
