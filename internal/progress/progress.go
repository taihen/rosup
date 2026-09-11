package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	markStart      = ">"
	markOK         = "*"
	markFail       = "x"
	markSkip       = "-"
	spinnerFrames  = `|/-\`
	maxDeviceWidth = 16
	maxLine        = 80
	spinInterval   = 100 * time.Millisecond
)

type flusher interface {
	Flush() error
}

type Printer struct {
	w       io.Writer
	tty     bool
	width   int
	mu      sync.Mutex
	stop    chan struct{}
	done    chan struct{}
	device  string
	label   string
	planned string
	lastN   int
	frame   int
}

func New(w io.Writer, devices []string) *Printer {
	if w == nil {
		return nil
	}
	return &Printer{
		w:     w,
		tty:   isTTY(w),
		width: deviceWidth(devices),
	}
}

func (p *Printer) Start(device, label string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
	p.device = device
	p.label = label
	p.planned = label
	p.frame = 0
	if p.tty {
		p.writeTTYLocked(spinnerFrame(0))
		p.stop = make(chan struct{})
		p.done = make(chan struct{})
		go p.spin(p.stop, p.done)
		return
	}
	p.writePlainLocked(markStart)
}

func (p *Printer) Update(label string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.tty || p.stop == nil {
		return
	}
	p.label = label
	p.writeTTYLocked(spinnerFrame(p.frame))
}

func (p *Printer) OK() { p.finish(markOK) }

func (p *Printer) Fail() { p.finish(markFail) }

func (p *Printer) Skip(device, label string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
	p.device = device
	p.label = label
	p.writePlainLocked(markSkip)
	p.lastN = 0
}

func (p *Printer) Track(device, label string, fn func() error) error {
	p.Start(device, label)
	if err := fn(); err != nil {
		p.Fail()
		return err
	}
	p.OK()
	return nil
}

func (p *Printer) Wait(sleep func(time.Duration), total time.Duration, tick func(left time.Duration)) {
	if p != nil && p.tty {
		tickWait(sleep, total, tick)
		return
	}
	if sleep != nil {
		sleep(total)
	}
}

func (p *Printer) finish(mark string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
	p.label = p.planned
	if !p.tty {
		p.writePlainLocked(mark)
		return
	}
	p.writeTTYLocked(mark)
	_, _ = io.WriteString(p.w, "\r"+p.line(mark)+"\n")
	p.lastN = 0
}

func (p *Printer) stopLocked() {
	if p.stop == nil {
		return
	}
	close(p.stop)
	done := p.done
	p.stop = nil
	p.done = nil
	p.mu.Unlock()
	<-done
	p.mu.Lock()
}

func (p *Printer) spin(stop, done chan struct{}) {
	defer close(done)
	tick := time.NewTicker(spinInterval)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			p.mu.Lock()
			if p.stop == stop {
				p.frame++
				p.writeTTYLocked(spinnerFrame(p.frame))
			}
			p.mu.Unlock()
		}
	}
}

func (p *Printer) writePlainLocked(mark string) {
	_, _ = io.WriteString(p.w, p.line(mark)+"\n")
}

func (p *Printer) writeTTYLocked(mark string) {
	line := p.line(mark)
	pad := max(0, p.lastN-len(line))
	_, _ = io.WriteString(p.w, "\r"+line+strings.Repeat(" ", pad))
	if f, ok := p.w.(flusher); ok {
		_ = f.Flush()
	}
	p.lastN = len(line)
}

func (p *Printer) line(mark string) string {
	prefix := mark + "  " + padDevice(p.device, p.width) + "  "
	return prefix + trunc(p.label, max(0, maxLine-len(prefix)))
}

func padDevice(name string, width int) string {
	if n := width - len(name); n > 0 {
		return name + strings.Repeat(" ", n)
	}
	return name
}

func deviceWidth(names []string) int {
	w := 0
	for _, n := range names {
		if l := len(n); l > w {
			w = l
		}
	}
	return min(w, maxDeviceWidth)
}

func trunc(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	if n <= 3 {
		return string(r[:n])
	}
	return string(r[:n-3]) + "..."
}

func spinnerFrame(i int) string {
	return string(spinnerFrames[i%len(spinnerFrames)])
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return false
	}
	termName := os.Getenv("TERM")
	return termName != "" && termName != "dumb"
}

func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	if m == 0 {
		return fmt.Sprintf("%ds", s)
	}
	if s == 0 {
		return fmt.Sprintf("%dm", m)
	}
	return fmt.Sprintf("%dm%ds", m, s)
}

func FormatLeft(left, total time.Duration) string {
	return FormatDuration(left) + " left of " + FormatDuration(total)
}

func tickWait(sleep func(time.Duration), total time.Duration, tick func(left time.Duration)) {
	if sleep == nil {
		return
	}
	remaining := total
	for remaining > 0 {
		if tick != nil {
			tick(remaining)
		}
		step := min(time.Second, remaining)
		sleep(step)
		remaining -= step
	}
}
