package cmd

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/term"
)

// progressEnabled is true when stderr is a TTY.
var progressEnabled = sync.OnceValue(func() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
})

// activeProgress is the currently displayed progress bar (if any).
// Protected by printMutex.
var activeProgress *progress

// clearActiveProgress clears the active progress bar from the terminal.
// Caller MUST hold printMutex.
func clearActiveProgress() {
	if activeProgress != nil && atomic.LoadInt32(&activeProgress.active) == 1 {
		fmt.Fprintf(os.Stderr, "\r\033[2K")
		atomic.StoreInt32(&activeProgress.active, 0)
	}
}

// redrawActiveProgress redraws the active progress bar after output.
// Caller MUST hold printMutex.
func redrawActiveProgress() {
	if activeProgress != nil && progressEnabled() {
		c := atomic.LoadInt64(&activeProgress.completed)
		if c < activeProgress.total {
			activeProgress.renderLocked(c)
		}
	}
}

// progress tracks completion of requests within a technique and renders
// an inline progress bar on stderr.
type progress struct {
	total     int64
	completed int64
	technique string
	barWidth  int
	active    int32 // 1 if currently showing a bar line
}

// newProgress creates a new progress tracker. Pass 0 total to disable.
func newProgress(technique string, total int) *progress {
	p := &progress{
		total:     int64(total),
		technique: technique,
		barWidth:  25,
	}
	if total > 0 && progressEnabled() {
		printMutex.Lock()
		activeProgress = p
		printMutex.Unlock()
	}
	return p
}

// done increments the completed counter and redraws the progress bar.
func (p *progress) done() {
	if p.total <= 0 || !progressEnabled() {
		return
	}
	c := atomic.AddInt64(&p.completed, 1)

	printMutex.Lock()
	defer printMutex.Unlock()
	p.renderLocked(c)
}

// finish clears the progress bar for good.
func (p *progress) finish() {
	if p.total <= 0 || !progressEnabled() {
		return
	}
	printMutex.Lock()
	defer printMutex.Unlock()
	if atomic.LoadInt32(&p.active) == 1 {
		fmt.Fprintf(os.Stderr, "\r\033[2K")
		atomic.StoreInt32(&p.active, 0)
	}
	if activeProgress == p {
		activeProgress = nil
	}
}

func (p *progress) renderLocked(completed int64) {
	total := p.total
	if total <= 0 {
		return
	}

	pct := float64(completed) / float64(total)
	if pct > 1 {
		pct = 1
	}

	filled := int(pct * float64(p.barWidth))
	empty := p.barWidth - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	fmt.Fprintf(os.Stderr, "\r\033[2K  %s %3.0f%% (%d/%d) %s", bar, pct*100, completed, total, p.technique)
	atomic.StoreInt32(&p.active, 1)
}
