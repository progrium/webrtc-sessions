package vad

import (
	"log"
	"math"
	"time"

	"github.com/progrium/webrtc-sessions/bridge/audio"
	"github.com/progrium/webrtc-sessions/bridge/tracks"
	"github.com/rs/xid"
)

type Annotator struct {
	sampleRateMs  int
	pcmWindowSize int

	energyThresh  float32
	silenceThresh float32

	vadGapSamples int
	maxPendingMs  int

	pcmWindow []float32
	windowID  string

	isSpeaking bool
	pendingMs  int
}

type CapturedAudio struct {
	ID string `json:"id"`

	PCM []float32 `json:"-"`

	Final bool `json:"final"`

	StartTimestamp uint64 `json:"start"`
	EndTimestamp   uint64 `json:"end"`
}

type CapturedSample struct {
	PCM          []float32
	EndTimestamp uint32
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
		pcmWindowSize: pcmWindowSize,
		pcmWindow:     make([]float32, 0, pcmWindowSize),
		isSpeaking:    false,
		vadGapSamples: sampleRateMs * 700,
		pendingMs:     0,
		maxPendingMs:  500,

		// this is an arbitrary number I picked after testing a bit
		// feel free to play around
		energyThresh:  0.0005,
		silenceThresh: 0.015,
	}
}

func (a *Annotator) Annotated(annot tracks.Annotation) {
	if annot.Type() != "audio" {
		return
	}
	pcm, err := audio.StreamAll(annot.Span().Audio())
	if err != nil {
		log.Println("vad:", err)
		return
	}
	start, ok := a.Push(pcm, annot.End)
	if ok {
		annot.Span().Span(start, annot.End).Annotate("activity", nil)
	}
}

func (a *Annotator) Push(pcm []float32, end tracks.Timestamp) (start tracks.Timestamp, ok bool) {
	if a.windowID == "" {
		a.windowID = xid.New().String()
	}

	if len(a.pcmWindow)+len(pcm) > a.pcmWindowSize {
		// This shouldn't happen hopefully...
		log.Printf("GOING TO OVERFLOW PCM WINDOW BY %d len(e.pcmWindow)=%d len(pcm)=%d e.pcmWindowSize=%d", len(a.pcmWindow)+len(pcm)-a.pcmWindowSize, len(a.pcmWindow), len(pcm), a.pcmWindowSize)
	}

	a.pcmWindow = append(a.pcmWindow, pcm...)
	a.pendingMs += len(pcm) / a.sampleRateMs

	flushFinal := false
	if len(a.pcmWindow) >= a.pcmWindowSize {
		flushFinal = true
	}

	// only look at the last N samples (at most) of pcmWindow, flush if we see silence there
	vadGapSamples := a.vadGapSamples
	vadStartIx := len(a.pcmWindow) - vadGapSamples
	if vadStartIx < 0 {
		vadStartIx = 0
	}

	wasSpeaking := a.isSpeaking
	isSpeaking, energy, silence := VAD(a.pcmWindow[vadStartIx:], a.energyThresh, a.silenceThresh)
	//log.Printf("isSpeaking %v energy %v silence %v", isSpeaking, energy, silence)
	if isSpeaking {
		a.isSpeaking = true
	}

	if len(a.pcmWindow) != 0 && !isSpeaking && wasSpeaking {
		// log.Printf("FINISHED SPEAKING")
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
		a.windowID = ""
		a.isSpeaking = false
		a.pcmWindow = a.pcmWindow[:0]
		a.pendingMs = 0

		_ = silence
		_ = energy
		// not speaking do nothing
		// Logger.Infof("NOT SPEAKING energy=%#v (energyThreshold=%#v) silence=%#v (silenceThreshold=%#v) endTimestamp=%d ", energy, e.energyThresh, silence, e.silenceThresh, endTimestamp)
		return end - tracks.Timestamp(len(a.pcmWindow)/a.sampleRateMs), true
	}

	if isSpeaking && wasSpeaking {
		//log.Printf("STILL SPEAKING")
	}

	if isSpeaking && !wasSpeaking {
		// log.Printf("STARTED SPEAKING")
	}

	flushDraft := false

	if a.pendingMs >= a.maxPendingMs && isSpeaking {
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
		a.pendingMs = 0
		return end - tracks.Timestamp(len(a.pcmWindow)/a.sampleRateMs), false
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
