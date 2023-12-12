package local

import (
	"sync"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

type Peer struct {
	*webrtc.PeerConnection
	ws   *websocket.Conn
	wsMu sync.Mutex
}

func NewPeer(url string) (*Peer, error) {
	conn, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}

	rtcpeer, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return nil, err
	}

	peer := &Peer{PeerConnection: rtcpeer, ws: conn}

	for _, typ := range []webrtc.RTPCodecType{webrtc.RTPCodecTypeVideo, webrtc.RTPCodecTypeAudio} {
		if _, err := rtcpeer.AddTransceiverFromKind(typ, webrtc.RTPTransceiverInit{
			Direction: webrtc.RTPTransceiverDirectionRecvonly,
		}); err != nil {
			peer.Close()
			return nil, err
		}
	}
}

func (p *Peer) Close() (err error) {
	err = p.PeerConnection.Close()
	if err != nil {
		return
	}
	return p.ws.Close()
}
