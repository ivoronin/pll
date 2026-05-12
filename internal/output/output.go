// Package output provides output buffering and progress rendering for parallel job execution.
package output

import (
	"bytes"
	"io"
	"os"

	"github.com/ivoronin/pll/internal/job"
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

// Output coordinates per-job writers and the optional progress bar around stderr/stdout.
type Output struct {
	mode Mode
	term *terminal
}

// New creates an Output with the specified buffering mode and no progress bar.
func New(mode Mode) *Output {
	return &Output{mode: mode, term: &terminal{}}
}

// EnableProgress activates a progress bar on stderr that reads from the given stats.
func (o *Output) EnableProgress(stats *job.Stats) {
	o.term.progress = newProgressBar(o.term, stats)
}

// RefreshProgress requests an immediate redraw of the progress bar (no-op when disabled).
func (o *Output) RefreshProgress() {
	if o.term.progress != nil {
		o.term.progress.refresh()
	}
}

// DoneProgress clears the progress bar and disables further drawing.
func (o *Output) DoneProgress() {
	if o.term.progress != nil {
		o.term.progress.Done()
	}
}

// NewWriters creates a new set of writers for a single job.
func (o *Output) NewWriters() *Writers {
	switch o.mode {
	case ModeNone:
		return &Writers{
			Stdout: o.wrapLocked(os.Stdout),
			Stderr: o.wrapLocked(os.Stderr),
			Flush:  func() {},
		}
	case ModeLine:
		return &Writers{
			Stdout: &lineWriter{term: o.term, dest: o.wrapDest(os.Stdout)},
			Stderr: &lineWriter{term: o.term, dest: o.wrapDest(os.Stderr)},
			Flush:  func() {},
		}
	case ModeJob:
		var stdoutBuf, stderrBuf bytes.Buffer

		return &Writers{
			Stdout: &stdoutBuf,
			Stderr: &stderrBuf,
			Flush: func() {
				o.term.mu.Lock()
				defer o.term.mu.Unlock()

				if o.term.progress != nil {
					o.term.progress.clear()
				}

				_, _ = stdoutBuf.WriteTo(os.Stdout)
				_, _ = stderrBuf.WriteTo(os.Stderr)

				if o.term.progress != nil {
					o.term.progress.draw()
				}
			},
		}
	default:
		return &Writers{Stdout: os.Stdout, Stderr: os.Stderr, Flush: func() {}}
	}
}

// wrapDest returns a clearingWriter if progress is enabled, otherwise returns dest as-is.
// The caller must hold the terminal mutex.
func (o *Output) wrapDest(dest io.Writer) io.Writer {
	if o.term.progress != nil {
		return &clearingWriter{dest: dest, term: o.term}
	}

	return dest
}

// wrapLocked returns a lockedWriter if progress is enabled, otherwise returns dest as-is.
// The writer acquires the terminal mutex itself.
func (o *Output) wrapLocked(dest io.Writer) io.Writer {
	if o.term.progress != nil {
		return &lockedWriter{dest: dest, term: o.term}
	}

	return dest
}

// lineWriter writes complete lines atomically under the terminal mutex.
type lineWriter struct {
	term *terminal
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

		lw.term.mu.Lock()
		_, writeErr := lw.dest.Write(line)
		lw.term.mu.Unlock()

		if writeErr != nil {
			return len(data), writeErr
		}

		lw.buf = lw.buf[idx+1:]
	}

	return len(data), nil
}
