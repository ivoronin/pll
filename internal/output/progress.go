package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/colorprofile"
	lipgloss "github.com/charmbracelet/lipgloss/v2"
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
	mu           *sync.Mutex
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

func newProgress(mutex *sync.Mutex, total int) *Progress {
	if !term.IsTerminal(os.Stderr.Fd()) {
		return &Progress{mu: mutex}
	}

	writer := colorprofile.NewWriter(os.Stderr, os.Environ())

	progress := &Progress{
		mu:         mutex,
		total:      total,
		enabled:    true,
		writer:     writer,
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
				progress.mu.Lock()
				progress.clear()
				progress.draw()
				progress.mu.Unlock()
			case <-progress.stopTicker:
				return
			}
		}
	}()

	progress.draw()

	return progress
}

// Inc increments the completion counter and redraws the bar.
func (p *Progress) Inc() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.enabled {
		return
	}

	p.done++
	p.clear()
	p.draw()
}

// Done clears the bar and disables further drawing.
func (p *Progress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ticker != nil {
		p.ticker.Stop()
		close(p.stopTicker)
	}

	p.clear()
	p.enabled = false
}

// clear erases the current progress bar line. Caller must hold the mutex.
func (p *Progress) clear() {
	if !p.enabled {
		return
	}

	_, _ = fmt.Fprint(os.Stderr, "\r\033[K")
}

// draw renders the progress bar on stderr. Caller must hold the mutex.
func (p *Progress) draw() {
	if !p.enabled {
		return
	}

	width := defaultWidth

	termWidth, _, err := term.GetSize(os.Stderr.Fd())
	if err == nil && termWidth > 0 {
		width = termWidth
	}

	suffix := fmt.Sprintf(" %d/%d", p.done, p.total)

	if len(suffix) > p.maxSuffixLen {
		p.maxSuffixLen = len(suffix)
	}

	suffix += strings.Repeat(" ", p.maxSuffixLen-len(suffix))

	barWidth := width - len(suffix)
	barWidth = max(barWidth, minBarWidth)

	filledCount := 0
	if p.total > 0 {
		filledCount = barWidth * p.done / p.total
	}

	emptyCount := barWidth - filledCount

	bar := p.filled.Render(strings.Repeat(barFilled, filledCount)) +
		p.empty.Render(strings.Repeat(barEmpty, emptyCount)) +
		p.text.Render(suffix)

	_, _ = lipgloss.Fprint(p.writer, bar)
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
	lw.progress.mu.Lock()
	defer lw.progress.mu.Unlock()

	lw.progress.clear()
	n, err := lw.dest.Write(data)
	lw.progress.draw()

	return n, err
}
