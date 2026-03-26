package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
)

const (
	barFilled    = "━"
	barEmpty     = "─"
	minBarWidth  = 5
	defaultWidth = 80
)

// Progress renders a terminal progress bar on stderr.
// It is safe for concurrent use. When stderr is not a TTY, all methods are no-ops.
type Progress struct {
	mutex        *sync.Mutex
	total        int
	done         int
	enabled      bool
	writer       *colorprofile.Writer
	filled       lipgloss.Style
	empty        lipgloss.Style
	text         lipgloss.Style
	maxSuffixLen int
	ticker       *time.Ticker
	stopTicker   chan struct{}
}

// Inc increments the completion counter and redraws the bar.
func (progress *Progress) Inc() {
	progress.mutex.Lock()
	defer progress.mutex.Unlock()

	if !progress.enabled {
		return
	}

	progress.done++
	progress.clear()
	progress.draw()
}

// Done clears the bar and disables further drawing.
func (progress *Progress) Done() {
	progress.mutex.Lock()
	defer progress.mutex.Unlock()

	if progress.ticker != nil {
		progress.ticker.Stop()
		close(progress.stopTicker)
	}

	progress.clear()
	progress.enabled = false
}

func newProgress(mutex *sync.Mutex, total int) *Progress {
	if !term.IsTerminal(os.Stderr.Fd()) {
		return &Progress{mutex: mutex}
	}

	colorWriter := colorprofile.NewWriter(os.Stderr, os.Environ())

	progress := &Progress{
		mutex:      mutex,
		total:      total,
		enabled:    true,
		writer:     colorWriter,
		filled:     lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
		empty:      lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
		text:       lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		ticker:     time.NewTicker(1 * time.Second),
		stopTicker: make(chan struct{}),
	}

	go func() {
		for {
			select {
			case <-progress.ticker.C:
				progress.mutex.Lock()
				progress.clear()
				progress.draw()
				progress.mutex.Unlock()
			case <-progress.stopTicker:
				return
			}
		}
	}()

	progress.draw()

	return progress
}

// clear erases the current progress bar line. Caller must hold the mutex.
func (progress *Progress) clear() {
	if !progress.enabled {
		return
	}

	_, _ = fmt.Fprint(os.Stderr, "\r\033[K")
}

// draw renders the progress bar on stderr. Caller must hold the mutex.
func (progress *Progress) draw() {
	if !progress.enabled {
		return
	}

	width := defaultWidth

	termWidth, _, sizeErr := term.GetSize(os.Stderr.Fd())
	if sizeErr == nil && termWidth > 0 {
		width = termWidth
	}

	suffix := fmt.Sprintf(" %d/%d", progress.done, progress.total)

	if len(suffix) > progress.maxSuffixLen {
		progress.maxSuffixLen = len(suffix)
	}

	suffix += strings.Repeat(" ", progress.maxSuffixLen-len(suffix))

	barWidth := width - len(suffix)
	barWidth = max(barWidth, minBarWidth)

	filledCount := 0
	if progress.total > 0 {
		filledCount = barWidth * progress.done / progress.total
	}

	emptyCount := barWidth - filledCount

	bar := progress.filled.Render(strings.Repeat(barFilled, filledCount)) +
		progress.empty.Render(strings.Repeat(barEmpty, emptyCount)) +
		progress.text.Render(suffix)

	_, _ = lipgloss.Fprint(progress.writer, bar)
}

// clearingWriter wraps a destination writer, clearing and redrawing the
// progress bar around each write. The caller must already hold the mutex.
type clearingWriter struct {
	dest     io.Writer
	progress *Progress
}

func (cw *clearingWriter) Write(data []byte) (int, error) {
	cw.progress.clear()
	n, err := cw.dest.Write(data)
	cw.progress.draw()

	return n, err
}

// lockedWriter acquires the mutex and clears/redraws around each write.
// Used for none-mode where there is no external mutex acquisition.
type lockedWriter struct {
	dest     io.Writer
	progress *Progress
}

func (lw *lockedWriter) Write(data []byte) (int, error) {
	lw.progress.mutex.Lock()
	defer lw.progress.mutex.Unlock()

	lw.progress.clear()
	n, err := lw.dest.Write(data)
	lw.progress.draw()

	return n, err
}
