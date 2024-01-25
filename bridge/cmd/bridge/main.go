package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/gopxl/beep"
	"github.com/gopxl/beep/speaker"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media/oggwriter"
	"github.com/progrium/webrtc-sessions/bridge/tracks"
	"github.com/progrium/webrtc-sessions/bridge/ui"
	"github.com/progrium/webrtc-sessions/bridge/vad"
	"github.com/progrium/webrtc-sessions/bridge/webrtc/js"
	"github.com/progrium/webrtc-sessions/bridge/webrtc/local"
	"github.com/progrium/webrtc-sessions/bridge/webrtc/sfu"
	"github.com/progrium/webrtc-sessions/bridge/webrtc/trackstreamer"
	"tractor.dev/toolkit-go/engine"
)

func main() {
	format := beep.Format{
		SampleRate:  beep.SampleRate(16000),
		NumChannels: 1,
		Precision:   4,
	}
	fatal(speaker.Init(format.SampleRate, format.SampleRate.N(time.Second/10)))

	engine.Run(
		Main{
			format: format,
		},
		vad.New(vad.Config{
			SampleRate:   format.SampleRate.N(time.Second),
			SampleWindow: 24 * time.Second,
		}),
	)
}

type Main struct {
	EventHandlers []tracks.Handler

	sessions map[string]*Session
	format   beep.Format

	mu sync.Mutex
}

type Session struct {
	*tracks.Session
	sfu  *sfu.Session
	peer *local.Peer
}

type View struct {
	Sessions []string
	Session  *Session
}

func fatal(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func (m *Main) TerminateDaemon(ctx context.Context) error {
	for id, sess := range m.sessions {
		b, err := cbor.Marshal(sess)
		if err != nil {
			return err
		}
		filename := fmt.Sprintf("./sessions/%s/session", id)
		if err := os.WriteFile(filename, b, 0644); err != nil {
			return err
		}
		// for debugging!
		// b, err = json.Marshal(sess)
		// if err != nil {
		// 	return err
		// }
		// filename = fmt.Sprintf("./sessions/%s/session.json", id)
		// if err := os.WriteFile(filename, b, 0644); err != nil {
		// 	return err
		// }
	}
	return nil
}

func (m *Main) SavedSessions() (names []string, err error) {
	dir, err := os.ReadDir("./sessions")
	if err != nil {
		return nil, err
	}
	for _, fi := range dir {
		if fi.IsDir() {
			names = append(names, fi.Name())
		}
	}
	return
}

func (m *Main) StartSession(sess *Session) {
	var err error
	sess.peer, err = local.NewPeer(fmt.Sprintf("ws://localhost:8088/sessions/%s?sfu", sess.ID)) // FIX: hardcoded host
	fatal(err)
	sess.peer.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		sessTrack := sess.NewTrack(m.format)

		log.Printf("got track %s %s", track.ID(), track.Kind())
		if track.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}
		ogg, err := oggwriter.New(fmt.Sprintf("./sessions/%s/track-%s.ogg", sess.ID, track.ID()), uint32(m.format.SampleRate.N(time.Second)), uint16(m.format.NumChannels))
		fatal(err)
		defer ogg.Close()
		rtp := trackstreamer.Tee(track, ogg)
		s, err := trackstreamer.New(rtp, m.format)
		fatal(err)

		chunkSize := sessTrack.AudioFormat().SampleRate.N(100 * time.Millisecond)
		for {
			// since Track.AddAudio expects finite segments, split it into chunks of
			// a smaller size we can append incrementally
			chunk := beep.Take(chunkSize, s)
			newstart := sessTrack.End()
			sessTrack.AddAudio(chunk)

			// seems like we should be doing this in session
			span := sessTrack.Span(newstart, sessTrack.End())
			e := span.RecordEvent("audio", nil)
			for _, antr := range m.EventHandlers {
				antr.HandleEvent(e)
			}

			fatal(chunk.Err())
			log.Printf("track %s: %v", sessTrack.ID, time.Duration(sessTrack.End()))
		}
	})
	sess.peer.HandleSignals()
}

func (m *Main) Serve(ctx context.Context) {
	m.sessions = make(map[string]*Session)

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	http.HandleFunc("/sessions", func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		sess := &Session{
			sfu:     sfu.NewSession(),
			Session: tracks.NewSession(),
		}
		m.sessions[string(sess.ID)] = sess
		m.mu.Unlock()
		fatal(os.MkdirAll(fmt.Sprintf("./sessions/%s", sess.ID), 0744))
		go m.StartSession(sess)
		http.Redirect(w, r, fmt.Sprintf("/sessions/%s", sess.ID), http.StatusFound)
	})

	http.HandleFunc("/sessions/", func(w http.ResponseWriter, r *http.Request) {
		sessID := filepath.Base(r.URL.Path)

		m.mu.Lock()
		sess, found := m.sessions[sessID]
		m.mu.Unlock()

		if !found {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if websocket.IsWebSocketUpgrade(r) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				log.Print("upgrade:", err)
				return
			}
			if r.URL.RawQuery == "sfu" {
				peer, err := sess.sfu.AddPeer(conn)
				if err != nil {
					log.Print("peer:", err)
					return
				}
				peer.HandleSignals()
			}
			if r.URL.RawQuery == "data" {
				for {
					names, err := m.SavedSessions()
					fatal(err)
					if err := conn.WriteJSON(View{
						Sessions: names,
						Session:  sess,
					}); err != nil {
						log.Println("data:", err)
						return
					}
					<-time.After(1 * time.Second)
				}
			}
			return
		}

		f, err := ui.Dir.Open("session.html")
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()
		http.ServeContent(w, r, "session.html", time.Now(), fileSeeker{f})
	})

	http.Handle("/webrtc/", http.StripPrefix("/webrtc", http.FileServer(http.FS(js.Dir))))
	http.Handle("/ui/", http.StripPrefix("/ui", http.FileServer(http.FS(ui.Dir))))
	http.Handle("/", http.RedirectHandler("/sessions", http.StatusFound))

	log.Println("running on http://localhost:8088 ...")
	log.Fatal(http.ListenAndServe(":8088", nil))
}

type fileSeeker struct {
	fs.File
}

func (fsk fileSeeker) Seek(offset int64, whence int) (int64, error) {
	if seeker, ok := fsk.File.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	return 0, io.ErrUnexpectedEOF
}
