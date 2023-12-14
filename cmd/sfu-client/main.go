package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
	"github.com/progrium/webrtc-sessions/local"
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

	videoTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: "video/vp8"}, "video", "pion")
	fatal(err)
	err = addTrack(ctx, peer, videoTrack)
	fatal(err)

	audioTrack, err := webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: "audio/opus"}, "audio", "pion")
	fatal(err)
	err = addTrack(ctx, peer, audioTrack)
	fatal(err)

	go rtpToTrack(ctx, videoTrack, &codecs.VP8Packet{}, 90000, 5004)
	go rtpToTrack(ctx, audioTrack, &codecs.OpusPacket{}, 48000, 5006)

	peer.HandleSignals()
}

func addTrack(ctx context.Context, peer *local.Peer, track webrtc.TrackLocal) error {
	sender, err := peer.AddTrack(track)
	if err != nil {
		return err
	}
	go processRTCP(ctx, sender)
	return nil
}

// Read incoming RTCP packets
// Before these packets are retuned they are processed by interceptors. For things
// like NACK this needs to be called.
func processRTCP(ctx context.Context, rtpSender *webrtc.RTPSender) {
	rtcpBuf := make([]byte, 1500)

	for {
		if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
			return
		}
	}
}

// Listen for incoming packets on a port and write them to a Track
func rtpToTrack(ctx context.Context, track *webrtc.TrackLocalStaticSample, depacketizer rtp.Depacketizer, sampleRate uint32, port int) {
	// Open a UDP Listener for RTP Packets on port 5004
	listener, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
	if err != nil {
		panic(err)
	}
	defer func() {
		if err = listener.Close(); err != nil {
			panic(err)
		}
	}()

	sampleBuffer := samplebuilder.New(10, depacketizer, sampleRate)

	// Read RTP packets forever and send them to the WebRTC Client
	for {
		inboundRTPPacket := make([]byte, 1500) // UDP MTU
		packet := &rtp.Packet{}

		n, _, err := listener.ReadFrom(inboundRTPPacket)
		if err != nil {
			panic(fmt.Sprintf("error during read: %s", err))
		}

		if err = packet.Unmarshal(inboundRTPPacket[:n]); err != nil {
			panic(err)
		}

		sampleBuffer.Push(packet)
		for {
			sample := sampleBuffer.Pop()
			if sample == nil {
				break
			}

			if writeErr := track.WriteSample(*sample); writeErr != nil {
				panic(writeErr)
			}
		}
	}
}
