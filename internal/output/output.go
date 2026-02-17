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
	mode     Mode
	mu       sync.Mutex
	progress *Progress
}

// NewFactory creates a Factory with the specified buffering mode.
func NewFactory(mode Mode) *Factory {
	return &Factory{mode: mode}
}

// EnableProgress activates a progress bar on stderr with the given total job count.
func (f *Factory) EnableProgress(total int) {
	f.progress = newProgress(&f.mu, total)
}

// IncProgress increments the progress bar by one completed job.
func (f *Factory) IncProgress() {
	if f.progress != nil {
		f.progress.Inc()
	}
}

// DoneProgress clears the progress bar and disables further drawing.
func (f *Factory) DoneProgress() {
	if f.progress != nil {
		f.progress.Done()
	}
}

// NewWriters creates a new set of writers for a single job.
func (f *Factory) NewWriters() *Writers {
	switch f.mode {
	case ModeNone:
		return &Writers{
			Stdout: f.wrapLocked(os.Stdout),
			Stderr: f.wrapLocked(os.Stderr),
			Flush:  func() {},
		}
	case ModeLine:
		return &Writers{
			Stdout: &lineWriter{mu: &f.mu, dest: f.wrapDest(os.Stdout)},
			Stderr: &lineWriter{mu: &f.mu, dest: f.wrapDest(os.Stderr)},
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

				if f.progress != nil {
					f.progress.clear()
				}

				_, _ = stdoutBuf.WriteTo(os.Stdout)
				_, _ = stderrBuf.WriteTo(os.Stderr)

				if f.progress != nil {
					f.progress.draw()
				}
			},
		}
	default:
		return &Writers{Stdout: os.Stdout, Stderr: os.Stderr, Flush: func() {}}
	}
}

// wrapDest returns a clearingWriter if progress is enabled, otherwise returns dest as-is.
// The caller must hold the mutex.
func (f *Factory) wrapDest(dest io.Writer) io.Writer {
	if f.progress != nil {
		return &clearingWriter{dest: dest, progress: f.progress}
	}

	return dest
}

// wrapLocked returns a lockedWriter if progress is enabled, otherwise returns dest as-is.
// The writer acquires the mutex itself.
func (f *Factory) wrapLocked(dest io.Writer) io.Writer {
	if f.progress != nil {
		return &lockedWriter{dest: dest, progress: f.progress}
	}

	return dest
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
