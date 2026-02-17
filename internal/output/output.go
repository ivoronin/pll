// Package output provides output buffering for parallel job execution.
package output

import (
	"bytes"
	"io"
	"os"
	"sync"
)

// Mode specifies the output buffering strategy.
type Mode string

const (
	// ModeNone passes output directly to stdout/stderr without buffering.
	ModeNone Mode = "none"
	// ModeLine buffers output line-by-line, writing complete lines atomically.
	ModeLine Mode = "line"
	// ModeJob buffers all output per job, flushing when the job completes.
	ModeJob Mode = "job"
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

// NewFactory creates a Factory with the specified buffering mode.
func NewFactory(mode Mode) *Factory {
	return &Factory{mode: mode}
}

// NewWriters creates a new set of writers for a single job.
func (f *Factory) NewWriters() *Writers {
	switch f.mode {
	case ModeNone:
		return &Writers{Stdout: os.Stdout, Stderr: os.Stderr, Flush: func() {}}
	case ModeLine:
		return &Writers{
			Stdout: &lineWriter{mu: &f.mu, dest: os.Stdout},
			Stderr: &lineWriter{mu: &f.mu, dest: os.Stderr},
			Flush:  func() {},
		}
	case ModeJob:
		var stdoutBuf, stderrBuf bytes.Buffer

		return &Writers{
			Stdout: &stdoutBuf,
			Stderr: &stderrBuf,
			Flush: func() {
				f.mu.Lock()
				defer f.mu.Unlock()

				_, _ = stdoutBuf.WriteTo(os.Stdout)
				_, _ = stderrBuf.WriteTo(os.Stderr)
			},
		}
	default:
		return &Writers{Stdout: os.Stdout, Stderr: os.Stderr, Flush: func() {}}
	}
}

// lineWriter writes complete lines atomically under a shared mutex.
type lineWriter struct {
	mu   *sync.Mutex
	dest io.Writer
	buf  []byte
}

func (lw *lineWriter) Write(data []byte) (int, error) {
	lw.buf = append(lw.buf, data...)

	for {
		idx := bytes.IndexByte(lw.buf, '\n')
		if idx < 0 {
			break
		}

		line := lw.buf[:idx+1]

		lw.mu.Lock()
		_, writeErr := lw.dest.Write(line)
		lw.mu.Unlock()

		if writeErr != nil {
			return len(data), writeErr
		}

		lw.buf = lw.buf[idx+1:]
	}

	return len(data), nil
}
