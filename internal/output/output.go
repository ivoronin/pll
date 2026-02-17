package output

import (
	"bytes"
	"io"
	"os"
	"sync"
)

type Mode string

const (
	ModeNone Mode = "none"
	ModeLine Mode = "line"
	ModeJob  Mode = "job"
)

// Writers holds stdout and stderr writers for a single job,
// along with a Flush function to call when the job completes.
type Writers struct {
	Stdout io.Writer
	Stderr io.Writer
	Flush  func()
}

// Factory creates Writers for each job based on the buffering mode.
type Factory struct {
	mode Mode
	mu   sync.Mutex
}

func NewFactory(mode Mode) *Factory {
	return &Factory{mode: mode}
}

func (f *Factory) NewWriters() *Writers {
	switch f.mode {
	case ModeNone:
		return &Writers{Stdout: os.Stdout, Stderr: os.Stderr, Flush: func() {}}
	case ModeLine:
		return &Writers{
			Stdout: &lineWriter{mu: &f.mu, w: os.Stdout},
			Stderr: &lineWriter{mu: &f.mu, w: os.Stderr},
			Flush:  func() {},
		}
	case ModeJob:
		var stdout, stderr bytes.Buffer
		return &Writers{
			Stdout: &stdout,
			Stderr: &stderr,
			Flush: func() {
				f.mu.Lock()
				defer f.mu.Unlock()
				stdout.WriteTo(os.Stdout)
				stderr.WriteTo(os.Stderr)
			},
		}
	default:
		return &Writers{Stdout: os.Stdout, Stderr: os.Stderr, Flush: func() {}}
	}
}

// lineWriter writes complete lines atomically under a shared mutex.
type lineWriter struct {
	mu  *sync.Mutex
	w   io.Writer
	buf []byte
}

func (lw *lineWriter) Write(p []byte) (int, error) {
	lw.buf = append(lw.buf, p...)
	for {
		idx := bytes.IndexByte(lw.buf, '\n')
		if idx < 0 {
			break
		}
		line := lw.buf[:idx+1]
		lw.mu.Lock()
		_, err := lw.w.Write(line)
		lw.mu.Unlock()
		if err != nil {
			return len(p), err
		}
		lw.buf = lw.buf[idx+1:]
	}
	return len(p), nil
}
