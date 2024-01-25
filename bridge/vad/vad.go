package vad

import (
	"log"
	"math"
	"sync"
	"time"

	"github.com/progrium/webrtc-sessions/bridge/audio"
	"github.com/progrium/webrtc-sessions/bridge/tracks"
	"github.com/rs/xid"
)

type Annotator struct {
	sampleRateMs  int
	maxWindowSize int

	energyThresh  float32
	silenceThresh float32

	vadGapSamples int
	maxPendingMs  int

	windows map[string]*Window
	mu      sync.Mutex
}

type Window struct {
	vad *Annotator

	pcm     []float32
	chunkID string

	isSpeaking bool
	pendingMs  int
}

type Config struct {
	// // This is determined by the hyperparameter configuration that whisper was trained on.
	// // See more here: https://github.com/ggerganov/whisper.cpp/issues/909
	SampleRate int //   = 16000 // 16kHz
	// sampleRateMs = SampleRate / 1000
	// // This determines how much audio we will be passing to whisper inference.
	// // We will buffer up to (whisperSampleWindowMs - pcmSampleRateMs) of old audio and then add
	// // audioSampleRateMs of new audio onto the end of the buffer for inference
	SampleWindow time.Duration // = 24000 // 24 second sample window
	// windowSize     = sampleWindowMs * sampleRateMs
	// // This determines how often we will try to run inference.
	// // We will buffer (pcmSampleRateMs * whisperSampleRate / 1000) samples and then run inference
	// pcmSampleRateMs = 500 // FIXME PLEASE MAKE ME AN CONFIG PARAM
	// pcmWindowSize   = pcmSampleRateMs * sampleRateMs
}

func New(config Config) *Annotator {
	sampleRateMs := config.SampleRate / 1000
	pcmWindowSize := int(config.SampleWindow.Seconds() * float64(config.SampleRate))
	return &Annotator{
		sampleRateMs:  sampleRateMs,
		maxWindowSize: pcmWindowSize,
		vadGapSamples: sampleRateMs * 700,
		maxPendingMs:  500,
		windows:       make(map[string]*Window),

		// this is an arbitrary number I picked after testing a bit
		// feel free to play around
		energyThresh:  0.0005,
		silenceThresh: 0.015,
	}
}

func (a *Annotator) Annotated(annot tracks.Annotation) {
	if annot.Type != "audio" {
		return
	}
	pcm, err := audio.StreamAll(annot.Span().Audio())
	if err != nil {
		log.Println("vad:", err)
		return
	}
	win := a.Window(string(annot.Span().Track().ID))
	start, ok := win.Push(pcm, annot.End)
	if ok {
		annot.Span().Span(start, annot.End).Annotate("activity", nil)
	}
}

func (a *Annotator) Window(name string) *Window {
	a.mu.Lock()
	w, ok := a.windows[name]
	if !ok {
		w = &Window{
			vad:        a,
			pendingMs:  0,
			pcm:        make([]float32, 0, a.maxWindowSize),
			isSpeaking: false,
		}
		a.windows[name] = w
	}
	a.mu.Unlock()
	return w
}

func (w *Window) Push(pcm []float32, end tracks.Timestamp) (start tracks.Timestamp, ok bool) {
	if w.chunkID == "" {
		w.chunkID = xid.New().String()
	}

	if len(w.pcm)+len(pcm) > w.vad.maxWindowSize {
		// This shouldn't happen hopefully...
		log.Printf("GOING TO OVERFLOW PCM WINDOW BY %d len(e.pcmWindow)=%d len(pcm)=%d e.pcmWindowSize=%d", len(w.pcm)+len(pcm)-w.vad.maxWindowSize, len(w.pcm), len(pcm), w.vad.maxWindowSize)
	}

	w.pcm = append(w.pcm, pcm...)
	w.pendingMs += len(pcm) / w.vad.sampleRateMs

	flushFinal := false
	if len(w.pcm) >= w.vad.maxWindowSize {
		flushFinal = true
	}

	// only look at the last N samples (at most) of pcmWindow, flush if we see silence there
	vadGapSamples := w.vad.vadGapSamples
	vadStartIx := len(w.pcm) - vadGapSamples
	if vadStartIx < 0 {
		vadStartIx = 0
	}

	wasSpeaking := w.isSpeaking
	isSpeaking, energy, silence := VAD(w.pcm[vadStartIx:], w.vad.energyThresh, w.vad.silenceThresh)
	//log.Printf("isSpeaking %v energy %v silence %v", isSpeaking, energy, silence)
	if isSpeaking {
		w.isSpeaking = true
	}

	if len(w.pcm) != 0 && !isSpeaking && wasSpeaking {
		log.Printf("FINISHED SPEAKING")
		flushFinal = true
	}

	if flushFinal {
		// cap := &CapturedAudio{
		// 	ID:           a.windowID,
		// 	Final:        true,
		// 	PCM:          append([]float32(nil), a.pcmWindow...),
		// 	EndTimestamp: uint64(endTimestamp),
		// 	// HACK surely there's a better way to calculate this?
		// 	StartTimestamp: uint64(endTimestamp) - uint64(len(a.pcmWindow)/a.sampleRateMs),
		// }
		w.chunkID = ""
		w.isSpeaking = false
		w.pcm = w.pcm[:0]
		w.pendingMs = 0

		_ = silence
		_ = energy
		// not speaking do nothing
		// Logger.Infof("NOT SPEAKING energy=%#v (energyThreshold=%#v) silence=%#v (silenceThreshold=%#v) endTimestamp=%d ", energy, e.energyThresh, silence, e.silenceThresh, endTimestamp)
		return end - tracks.Timestamp(len(w.pcm)/w.vad.sampleRateMs), true
	}

	if isSpeaking && wasSpeaking {
		//log.Printf("STILL SPEAKING")
	}

	if isSpeaking && !wasSpeaking {
		log.Printf("STARTED SPEAKING")
	}

	flushDraft := false

	if w.pendingMs >= w.vad.maxPendingMs && isSpeaking {
		flushDraft = true
	}

	if flushDraft {
		// cap := &CapturedAudio{
		// 	ID:           a.windowID,
		// 	Final:        false,
		// 	PCM:          append([]float32(nil), a.pcmWindow...),
		// 	EndTimestamp: uint64(endTimestamp),
		// 	// HACK surely there's a better way to calculate this?
		// 	StartTimestamp: uint64(endTimestamp) - uint64(len(a.pcmWindow)/a.sampleRateMs),
		// }
		w.pendingMs = 0
		return end - tracks.Timestamp(len(w.pcm)/w.vad.sampleRateMs), false
	}

	return 0, false
}

// NOTE This is a very rough implemntation. We should improve it :D
// VAD performs voice activity detection on a frame of audio data.
func VAD(frame []float32, energyThresh, silenceThresh float32) (bool, float32, float32) {
	// Compute frame energy
	energy := float32(0)
	for i := 0; i < len(frame); i++ {
		energy += frame[i] * frame[i]
	}
	energy /= float32(len(frame))

	// Compute frame silence
	silence := float32(0)
	for i := 0; i < len(frame); i++ {
		silence += float32(math.Abs(float64(frame[i])))
	}
	silence /= float32(len(frame))

	// Apply energy threshold
	if energy < energyThresh {
		return false, energy, silence
	}

	// Apply silence threshold
	if silence < silenceThresh {
		return false, energy, silence
	}

	return true, energy, silence
}
