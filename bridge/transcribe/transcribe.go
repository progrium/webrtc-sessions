package transcribe

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/gopxl/beep"
	"github.com/progrium/webrtc-sessions/bridge"
)

type Service struct {
	pipe io.WriteCloser
	out  chan []map[string]any
	mu   sync.Mutex
}

func (s *Service) Transcribe(samples []float32, format beep.Format) []bridge.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pipe == nil {
		return nil
	}
	buf := new(bytes.Buffer)
	for _, sample := range samples {
		err := binary.Write(buf, binary.LittleEndian, sample)
		if err != nil {
			log.Fatal("binary.Write failed:", err)
		}
	}
	fmt.Fprintf(s.pipe, "%s\n", strconv.Itoa(buf.Len()))
	_, err := buf.WriteTo(s.pipe)
	if err != nil {
		log.Fatal(err)
	}
	var spans []bridge.Span
	for _, word := range <-s.out {
		spans = append(spans, bridge.Span{
			Text:  word["word"].(string),
			Start: format.SampleRate.N(time.Duration(word["start"].(float64) * float64(time.Second))),
			End:   format.SampleRate.N(time.Duration(word["end"].(float64) * float64(time.Second))),
			Prob:  word["prob"].(float64),
		})
	}
	return spans
}

func (s *Service) Serve(ctx context.Context) {
	s.out = make(chan []map[string]any)
	_, filename, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(filename), "transcribe.py")

	cmd := exec.CommandContext(ctx, "python3.8", "-u", script)
	cmd.Stderr = os.Stderr
	rc, _ := cmd.StdoutPipe()
	s.pipe, _ = cmd.StdinPipe()

	cmd.Start()

	go func() {
		scanner := bufio.NewScanner(rc)
		for scanner.Scan() {
			d := map[string]any{}
			if err := json.Unmarshal(scanner.Bytes(), &d); err != nil {
				log.Fatal(err)
			}
			var spans []map[string]any
			for _, o := range d["segments"].([]any) {
				segment := o.(map[string]any)
				for _, oo := range segment["words"].([]any) {
					spans = append(spans, oo.(map[string]any))
				}

			}
			// fmt.Println(spans)
			s.out <- spans
		}
	}()

	if err := cmd.Wait(); err != nil {
		log.Println(err)
	}
}
