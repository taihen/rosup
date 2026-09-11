package progress_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/taihen/rosup/internal/progress"
)

func TestPlainStartOK(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf, []string{"boa"})
	p.Start("boa", "checking SSH and version")
	p.OK()
	want := "" +
		">  boa  checking SSH and version\n" +
		"*  boa  checking SSH and version\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}

func TestPlainFail(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf, []string{"python"})
	p.Start("python", "installing packages")
	p.Fail()
	want := "" +
		">  python  installing packages\n" +
		"x  python  installing packages\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}

func TestPlainSkipHasNoStartLine(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf, []string{"boa"})
	p.Skip("boa", "already 6.49.21")
	want := "-  boa  already 6.49.21\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}

func TestNilPrinterIsNoop(t *testing.T) {
	var p *progress.Printer
	p.Start("boa", "checking SSH and version")
	p.Update("waiting")
	p.OK()
	p.Fail()
	p.Skip("boa", "already 6.49.21")
}

func TestPadsShorterDeviceNames(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf, []string{"boa", "SAUZA2"})
	p.Skip("boa", "already 6.49.21")
	if !strings.HasPrefix(buf.String(), "-  boa     already 6.49.21\n") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestOKUsesStartLabelNotUpdate(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf, []string{"python"})
	p.Start("python", "waiting for SSH (3m)")
	p.Update("waiting for SSH (2m12s left of 3m)")
	p.OK()
	if !strings.Contains(buf.String(), "*  python  waiting for SSH (3m)\n") {
		t.Fatalf("got %q", buf.String())
	}
	if strings.Contains(buf.String(), "left of") {
		t.Fatalf("plain mode should not print countdown ticks: %q", buf.String())
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Minute, "5m"},
		{3 * time.Minute, "3m"},
		{5*time.Minute + 12*time.Second, "5m12s"},
		{45 * time.Second, "45s"},
		{0, "0s"},
	}
	for _, tc := range cases {
		if got := progress.FormatDuration(tc.d); got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.d, got, tc.want)
		}
	}
}

func TestFormatLeft(t *testing.T) {
	got := progress.FormatLeft(4*time.Minute+12*time.Second, 5*time.Minute)
	if got != "4m12s left of 5m" {
		t.Fatalf("got %q", got)
	}
	got = progress.FormatLeft(45*time.Second, 5*time.Minute)
	if got != "45s left of 5m" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncatesLabelTo80Columns(t *testing.T) {
	var buf bytes.Buffer
	p := progress.New(&buf, []string{"boa"})
	p.Skip("boa", strings.Repeat("a", 200))
	line := strings.TrimSuffix(buf.String(), "\n")
	if len(line) != 80 {
		t.Fatalf("len %d, want 80: %q", len(line), line)
	}
	if !strings.HasSuffix(line, "...") {
		t.Fatalf("want truncated label: %q", line)
	}
}
