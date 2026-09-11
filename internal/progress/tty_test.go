package progress

import (
	"bytes"
	"strings"
	"testing"
)

func TestTTYSettlesPlannedLabelNotCountdown(t *testing.T) {
	var buf bytes.Buffer
	p := New(&buf, []string{"python"})
	p.tty = true
	p.Start("python", "waiting for SSH (3m)")
	p.Update("waiting for SSH (2m12s left of 3m)")
	p.OK()

	out := buf.String()
	if !strings.Contains(out, "\r") {
		t.Fatalf("TTY should rewrite with CR: %q", out)
	}
	if strings.Contains(out, ">") {
		t.Fatalf("TTY should spin, not print >: %q", out)
	}
	i := strings.LastIndex(out, "\r")
	final := out[i+1:]
	if final != "*  python  waiting for SSH (3m)\n" {
		t.Fatalf("settled %q", final)
	}
}
