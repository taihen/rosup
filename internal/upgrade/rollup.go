package upgrade

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/taihen/rosup/internal/inventory"
	"github.com/taihen/rosup/internal/state"
)

// Rollup summarizes selected-device job state for one release.
type Rollup struct {
	Release  string
	Complete int
	Failed   int
	Pending  int
}

// CountRollup classifies each selected device's job for version.
func CountRollup(stateDir, version string, devices []inventory.Device) (Rollup, error) {
	r := Rollup{Release: version}
	for _, d := range devices {
		job, err := state.Load(stateDir, d.Name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				r.Pending++
				continue
			}
			return Rollup{}, err
		}
		switch {
		case job.Status == state.StatusComplete && job.Release == version:
			r.Complete++
		case job.Status == state.StatusFailed && (job.Release == "" || job.Release == version):
			r.Failed++
		default:
			r.Pending++
		}
	}
	return r, nil
}

// WriteRollup prints a plan-style one-line completion summary.
func WriteRollup(w io.Writer, r Rollup) error {
	if w == nil {
		return nil
	}
	status := padRight("OK", len("FAILED"))
	if r.Failed > 0 || r.Pending > 0 {
		status = "FAILED"
	}
	_, err := fmt.Fprintf(w, "upgrade: %s  %d complete / %d failed / %d pending  release %s\n",
		status, r.Complete, r.Failed, r.Pending, r.Release)
	return err
}

// Incomplete returns a counts-only error when any selected device is not complete.
func Incomplete(r Rollup) error {
	if r.Failed == 0 && r.Pending == 0 {
		return nil
	}
	return fmt.Errorf("upgrade: incomplete (%d complete / %d failed / %d pending)",
		r.Complete, r.Failed, r.Pending)
}

func padRight(s string, width int) string {
	if width <= len(s) {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
