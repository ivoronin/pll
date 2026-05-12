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

	"github.com/ivoronin/pll/internal/job"
)

const (
	barFilled    = "━"
	barEmpty     = "─"
	minBarWidth  = 5
	defaultWidth = 80
)

// terminal owns the synchronization point and the optional progress bar
// for everything that touches stderr (writers and the bar itself).
type terminal struct {
	mu       sync.Mutex
	progress *progressBar
}

// progressBar renders a terminal progress bar on stderr.
// Counts are read from the shared *job.Stats under no extra lock (atomics).
// When stderr is not a TTY, all methods are no-ops.
type progressBar struct {
	term         *terminal
	stats        *job.Stats
	enabled      bool
	writer       *colorprofile.Writer
	filled       lipgloss.Style
	empty        lipgloss.Style
	text         lipgloss.Style
	maxSuffixLen int
	ticker       *time.Ticker
	stopTicker   chan struct{}
}

// Done clears the bar and disables further drawing.
func (pb *progressBar) Done() {
	pb.term.mu.Lock()
	defer pb.term.mu.Unlock()

	if pb.ticker != nil {
		pb.ticker.Stop()
		close(pb.stopTicker)
	}

	pb.clear()
	pb.enabled = false
}

// refresh redraws the bar from current counter snapshot.
func (pb *progressBar) refresh() {
	pb.term.mu.Lock()
	defer pb.term.mu.Unlock()

	if !pb.enabled {
		return
	}

	pb.clear()
	pb.draw()
}

func (pb *progressBar) formatSuffix(snap job.StatsSnapshot) string {
	done := snap.Succeeded + snap.Failed + snap.TimedOut + snap.Skipped
	suffix := fmt.Sprintf(" %d / %d", done, snap.Total)

	if done == 0 {
		return suffix
	}

	var parts []string
	if snap.Succeeded > 0 {
		parts = append(parts, fmt.Sprintf("%d done", snap.Succeeded))
	}

	if snap.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", snap.Failed))
	}

	if snap.TimedOut > 0 {
		parts = append(parts, fmt.Sprintf("%d timed out", snap.TimedOut))
	}

	if snap.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", snap.Skipped))
	}

	return suffix + " (" + strings.Join(parts, ", ") + ")"
}

func newProgressBar(term *terminal, stats *job.Stats) *progressBar {
	if !isTerminalStderr() {
		return &progressBar{term: term, stats: stats}
	}

	colorWriter := colorprofile.NewWriter(os.Stderr, os.Environ())

	bar := &progressBar{
		term:       term,
		stats:      stats,
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
			case <-bar.ticker.C:
				bar.term.mu.Lock()
				bar.clear()
				bar.draw()
				bar.term.mu.Unlock()
			case <-bar.stopTicker:
				return
			}
		}
	}()

	bar.draw()

	return bar
}

func isTerminalStderr() bool {
	return term.IsTerminal(os.Stderr.Fd())
}

// clear erases the current progress bar line. Caller must hold the mutex.
func (pb *progressBar) clear() {
	if !pb.enabled {
		return
	}

	_, _ = fmt.Fprint(os.Stderr, "\r\033[K")
}

// draw renders the progress bar on stderr. Caller must hold the mutex.
func (pb *progressBar) draw() {
	if !pb.enabled {
		return
	}

	width := defaultWidth

	termWidth, _, sizeErr := term.GetSize(os.Stderr.Fd())
	if sizeErr == nil && termWidth > 0 {
		width = termWidth
	}

	snap := pb.stats.Snapshot()
	suffix := pb.formatSuffix(snap)

	if len(suffix) > pb.maxSuffixLen {
		pb.maxSuffixLen = len(suffix)
	}

	suffix += strings.Repeat(" ", pb.maxSuffixLen-len(suffix))

	barWidth := width - len(suffix)
	barWidth = max(barWidth, minBarWidth)

	done := snap.Succeeded + snap.Failed + snap.TimedOut + snap.Skipped

	filledCount := 0
	if snap.Total > 0 {
		filledCount = barWidth * done / snap.Total
	}

	emptyCount := barWidth - filledCount

	bar := pb.filled.Render(strings.Repeat(barFilled, filledCount)) +
		pb.empty.Render(strings.Repeat(barEmpty, emptyCount)) +
		pb.text.Render(suffix)

	_, _ = lipgloss.Fprint(pb.writer, bar)
}

// clearingWriter wraps a destination writer, clearing and redrawing the
// progress bar around each write. The caller must already hold the terminal mutex.
type clearingWriter struct {
	dest io.Writer
	term *terminal
}

func (cw *clearingWriter) Write(data []byte) (int, error) {
	cw.term.progress.clear()
	n, err := cw.dest.Write(data)
	cw.term.progress.draw()

	return n, err
}

// lockedWriter acquires the terminal mutex and clears/redraws around each write.
// Used for none-mode where there is no external mutex acquisition.
type lockedWriter struct {
	dest io.Writer
	term *terminal
}

func (lw *lockedWriter) Write(data []byte) (int, error) {
	lw.term.mu.Lock()
	defer lw.term.mu.Unlock()

	lw.term.progress.clear()
	n, err := lw.dest.Write(data)
	lw.term.progress.draw()

	return n, err
}
