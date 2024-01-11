package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/effects"
	"github.com/gopxl/beep/speaker"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/progrium/webrtc-sessions/bridge"
	"github.com/progrium/webrtc-sessions/bridge/diarize"
	"github.com/progrium/webrtc-sessions/bridge/tracks"
	"github.com/progrium/webrtc-sessions/bridge/transcribe"
	"github.com/progrium/webrtc-sessions/local"
	"github.com/progrium/webrtc-sessions/sfu"
	"github.com/progrium/webrtc-sessions/trackstreamer"
	"github.com/progrium/webrtc-sessions/vad"
	"tractor.dev/toolkit-go/engine"
)

func main() {
	engine.Run(Main{}, sfu.Service{}, transcribe.Service{}, diarize.Service{})
}

type Main struct {
	STT       *transcribe.Service
	Diarizer  *diarize.Service
	frames    []*bridge.AudioFrame
	format    beep.Format
	diarizing bool
	mu        sync.Mutex
}

func fatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func (m *Main) TerminateDaemon(ctx context.Context) error {
	if len(m.frames) == 0 {
		return nil
	}
	b, err := cbor.Marshal(m.frames)
	if err != nil {
		return err
	}
	filename := fmt.Sprintf("session-%d", m.frames[0].Timestamp)
	return os.WriteFile(filename, b, 0644)
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

	m.format = beep.Format{
		SampleRate:  beep.SampleRate(16000),
		NumChannels: 1,
		Precision:   4,
	}
	fatal(speaker.Init(m.format.SampleRate, m.format.SampleRate.N(time.Second/10)))

	session := tracks.NewSession()

	peer.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		sessTrack := session.NewTrack(m.format)

		log.Printf("got track %s %s", track.ID(), track.Kind())
		if track.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		ogg, err := oggwriter.New(fmt.Sprintf("track-%s.ogg", track.ID()), uint32(m.format.SampleRate.N(time.Second)), uint16(m.format.NumChannels))
		fatal(err)
		defer ogg.Close()
		rtp := trackstreamer.Tee(track, ogg)
		s, err := trackstreamer.New(rtp, m.format)
		fatal(err)

		s, s2 := beep.Dup(s)
		go func() {
			chunkSize := sessTrack.AudioFormat().SampleRate.N(100 * time.Millisecond)
			for {
				// since Track.AddAudio expects finite segments, split it into chunks of
				// a smaller size we can append incrementally
				chunk := beep.Take(chunkSize, s2)
				sessTrack.AddAudio(chunk)
				fatal(chunk.Err())
				log.Printf("track %s: %v", sessTrack.ID, time.Duration(sessTrack.End()))
			}
		}()

		detector := vad.New(vad.Config{
			SampleRate:   m.format.SampleRate.N(time.Second),
			SampleWindow: 24 * time.Second,
		})
		s = effects.Mono(s) // should already be mono, but we can make sure
		// s = &effects.Volume{Streamer: s, Base: 2, Volume: 2}
		var totSamples int
		for {
			pcm, err := Stream32(s, 1024)
			if err != nil {
				fatal(err)
			}
			totSamples += len(pcm)
			out := detector.Push(&vad.CapturedSample{
				PCM:          pcm,
				EndTimestamp: uint32(m.format.SampleRate.D(totSamples) / time.Millisecond),
			})
			if out != nil {
				// log.Printf("got output chunk of %d samples. final: %v", len(out.PCM), out.Final)
				if out.Final {
					go m.HandleFrame(&bridge.AudioFrame{
						Audio:     NewBufferFromSamples(m.format, out.PCM),
						Timestamp: int(time.Now().Unix()),
					})
				}
			}
		}
	})

	peer.HandleSignals()
}

// SpeakerContext creates a stream of up to 20 words from each
// known speaker in order of appearance for use as context for new diarizations
// to maintain speaker identity. Unfortunately, since diarization
// isn't always consistent in labeling speakers, this won't always work.
func (m *Main) SpeakerContext() beep.Streamer {
	m.mu.Lock()
	frames := m.frames[:]
	m.mu.Unlock()
	var speakers []string
	speakerSamples := map[string]*beep.Buffer{}
	speakerSampleWords := map[string]int{}
	curSpeaker := ""
	for _, frame := range frames {
		for _, word := range frame.WordSpans {
			if word.Prob < 0.5 {
				continue
			}
			if curSpeaker != word.Speaker && word.Speaker != "" {
				curSpeaker = word.Speaker
				buf, ok := speakerSamples[curSpeaker]
				if !ok {
					buf = beep.NewBuffer(m.format)
					speakerSamples[curSpeaker] = buf
					speakers = append(speakers, curSpeaker)
				}
			}
			if curSpeaker != "" && speakerSampleWords[curSpeaker] < 20 {
				buf := speakerSamples[curSpeaker]
				buf.Append(frame.Audio.Streamer(word.Start, word.End))
				speakerSampleWords[curSpeaker]++
			}
		}
	}
	var clips []beep.Streamer
	for _, name := range speakers {
		clips = append(clips, speakerSamples[name].Streamer(0, speakerSamples[name].Len()))
	}
	return beep.Seq(clips...)
}

func (m *Main) DiarizeFrames() {
	m.mu.Lock()
	if m.diarizing {
		m.mu.Unlock()
		return
	}
	m.diarizing = true
	frameCount := len(m.frames)
	var toDiarize []*bridge.AudioFrame
	for _, f := range m.frames {
		if !f.Diarized {
			toDiarize = append(toDiarize, f)
		}
	}
	log.Println("DIARIZING", len(toDiarize), "...")
	m.mu.Unlock()
	buf := beep.NewBuffer(m.format)
	for _, frame := range toDiarize {
		frame.Diarized = true
		buf.Append(frame.Audio.Streamer(frame.WordsStart(), frame.WordsEnd()))
	}
	samples, err := StreamAll32(buf.Streamer(0, buf.Len()))
	if err != nil {
		log.Fatal(err)
	}
	contextSamples, ctxErr := StreamAll32(m.SpeakerContext())
	if ctxErr != nil {
		log.Fatal(err)
	}
	spans := m.Diarizer.Diarize(append(contextSamples, samples...), m.format)

	ctxLen := len(contextSamples)
	frameStart := 0
	for _, frame := range toDiarize {
		for wordIdx, word := range frame.WordSpans {
			wordLen := word.End - word.Start
			wordMid := word.Start + (wordLen / 2) - frame.WordsStart()
			for _, span := range spans {
				if (frameStart+wordMid) >= (span.Start-ctxLen) && (frameStart+wordMid) <= (span.End-ctxLen) {
					frame.WordSpans[wordIdx].Speaker = span.Speaker
				}
			}
		}
		frameStart += (frame.WordsEnd() - frame.WordsStart())
		//fmt.Println(frame.WordSpans)
	}

	fmt.Println("NEW DIARIZATION")
	for _, m := range m.Messages() {
		fmt.Printf("%s: %s\n", m.From, m.Text)
	}

	m.mu.Lock()
	m.diarizing = false
	defer m.mu.Unlock()
	if len(m.frames) > frameCount {
		go m.DiarizeFrames()
	}
}

func (m *Main) Messages() (msgs []bridge.Message) {
	m.mu.Lock()
	frames := m.frames[:]
	m.mu.Unlock()
	curSpeaker := ""
	identMap := map[string]string{}
	msgBuf := ""

	for _, frame := range frames {
		for wordIdx, word := range frame.WordSpans {
			wordSpeaker := word.Speaker
			if wordSpeaker == "" && wordIdx == 0 {
				// sometimes first word of frame
				// won't get a speaker due to reasons
				if len(frame.WordSpans) > wordIdx+1 {
					wordSpeaker = frame.WordSpans[wordIdx+1].Speaker
				}
			}
			if wordSpeaker != "" && wordSpeaker != curSpeaker {
				if msgBuf != "" {
					msgs = append(msgs, bridge.Message{
						From: curSpeaker,
						Text: msgBuf,
					})
					msgBuf = ""
				}
				curSpeaker = wordSpeaker
			}
			_, exists := identMap[curSpeaker]
			if frame.Ident != "" && !exists {
				// first speaker of frame gets frame identity
				identMap[curSpeaker] = frame.Ident
			}
			msgBuf += word.Text
		}
		if msgBuf != "" {
			msgs = append(msgs, bridge.Message{
				From: curSpeaker,
				Text: msgBuf,
			})
			msgBuf = ""
		}
	}
	identity := func(speaker string) string {
		id, ok := identMap[speaker]
		if ok {
			return id
		}
		return speaker
	}
	for idx, _ := range msgs {
		msgs[idx].From = identity(msgs[idx].From)
	}
	return
}

func (m *Main) HandleFrame(frame *bridge.AudioFrame) {
	samples, err := StreamAll32(frame.Audio.Streamer(0, frame.Audio.Len()))
	if err != nil {
		log.Fatal(err)
	}

	frame.WordSpans = m.STT.Transcribe(samples, m.format)

	if len(frame.WordSpans) > 0 && frame.WordSpans[0].Prob < 0.1 {
		return
	}

	var text string
	for _, span := range frame.WordSpans {
		text += span.Text
	}
	if text == "" {
		return
	}

	m.mu.Lock()
	m.frames = append(m.frames, frame)
	if !m.diarizing {
		go m.DiarizeFrames()
	}
	m.mu.Unlock()
	fmt.Println("GOT:", text)
	cmdText := strings.ToLower(text)
	// if strings.Contains(cmdText, "go speakers") {
	// 	fmt.Println("DETECTING SPEAKERS...")
	// 	go m.DiarizeFrames()
	// }
	if strings.Contains(cmdText, "exit program") {
		engine.Terminate()
	}
	if strings.Contains(cmdText, "my name is") {
		re := regexp.MustCompile(`my name is (\w+)`)
		matches := re.FindStringSubmatch(cmdText)
		if len(matches) < 2 {
			return
		}
		frame.Ident = matches[1]
		fmt.Println("SAVED IDENTITY", matches[1])
	}

}

type Float32Stream struct {
	Samples []float32
	cur     int
}

func (s *Float32Stream) Stream(samples [][2]float64) (n int, ok bool) {
	for i := range samples {
		if s.cur >= len(s.Samples) {
			return i, false
		}
		sample := float64(s.Samples[s.cur])
		samples[i][0], samples[i][1] = sample, sample
		s.cur++
	}
	return len(samples), true
}

func (s *Float32Stream) Err() error {
	return nil
}

func Stream32(streamer beep.Streamer, numSamples int) ([]float32, error) {
	buffer := make([][2]float64, numSamples)
	n, ok := streamer.Stream(buffer)
	if !ok && n == 0 {
		return nil, streamer.Err()
	}

	samples := make([]float32, n)
	for i := 0; i < n; i++ {
		samples[i] = float32((buffer[i][0] + buffer[i][1]) / 2)
	}

	return samples, nil
}

func StreamAll32(streamer beep.Streamer) ([]float32, error) {
	var allSamples []float32

	for {
		buffer := make([][2]float64, 1024) // Buffer size can be adjusted
		n, ok := streamer.Stream(buffer)
		if !ok {
			if err := streamer.Err(); err != nil {
				return nil, err
			}
			if n == 0 {
				break
			}
		}

		for i := 0; i < n; i++ {
			allSamples = append(allSamples, float32((buffer[i][0]+buffer[i][1])/2))
		}
	}

	return allSamples, nil
}

func NewBufferFromSamples(format beep.Format, samples []float32) *beep.Buffer {
	buf := beep.NewBuffer(format)
	buf.Append(&Float32Stream{Samples: samples})
	return buf
}
