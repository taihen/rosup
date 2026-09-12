package preflight

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/release"
)

const diskMarginBytes int64 = 1 << 20

var sizeRe = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*(b|kib|mib|gib|tib)?$`)

type DiskError struct {
	Have, Need int64
}

func (e *DiskError) Error() string {
	return fmt.Sprintf("preflight: not enough free disk: have %d bytes, need %d bytes (staged packages + 1 MiB)", e.Have, e.Need)
}

type MissingPackagesError struct {
	Arch     string
	Packages []string
}

func (e *MissingPackagesError) Error() string {
	return fmt.Sprintf("preflight: missing packages for architecture %s: %s", e.Arch, strings.Join(e.Packages, ", "))
}

type UnsupportedError struct {
	Version string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("preflight: RouterOS 7 is not supported (%s)", e.Version)
}

// FormatSize renders binary units for the plan report (no raw bytes).
func FormatSize(n int64) string {
	if n < 0 {
		n = 0
	}
	const (
		kib = 1024
		mib = 1024 * 1024
		gib = 1024 * 1024 * 1024
	)
	switch {
	case n >= gib:
		return formatUnit(float64(n)/float64(gib), "GiB")
	case n >= mib:
		return formatUnit(float64(n)/float64(mib), "MiB")
	case n >= kib:
		return formatUnit(float64(n)/float64(kib), "KiB")
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func formatUnit(v float64, unit string) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f %s", v, unit)
	}
	return fmt.Sprintf("%.1f %s", v, unit)
}

func VersionMatches(facts discover.Facts, version string) bool {
	return facts.Version != "" && facts.Version == version
}

func AlreadyOnRelease(facts discover.Facts, version string) bool {
	if !VersionMatches(facts, version) {
		return false
	}
	if len(facts.Packages) == 0 {
		return false
	}
	for _, p := range facts.Packages {
		if p.Version != version {
			return false
		}
	}
	return true
}

func Check(facts discover.Facts, man release.Manifest) error {
	if err := rejectROS7(facts.Version); err != nil {
		return err
	}
	if err := rejectROS7(man.Version); err != nil {
		return err
	}
	// Disk check is only for staging packages. Matching system version means
	// packages are already installed enough that free space is not a gate.
	if VersionMatches(facts, man.Version) {
		return nil
	}

	byPkg := release.IndexByInstalledName(man.Files, facts.ArchitectureName)

	var stagedSize int64
	var missing []string
	for _, p := range facts.Packages {
		f, ok := byPkg[p.Name]
		if !ok {
			missing = append(missing, p.Name)
			continue
		}
		stagedSize += f.Size
	}
	if len(missing) > 0 {
		return &MissingPackagesError{Arch: facts.ArchitectureName, Packages: missing}
	}

	free, err := parseSize(facts.FreeHDDSpace)
	if err != nil {
		return fmt.Errorf("preflight: parse free-hdd-space %q: %w", facts.FreeHDDSpace, err)
	}
	need := stagedSize + diskMarginBytes
	if free < need {
		return &DiskError{Have: free, Need: need}
	}
	return nil
}

func rejectROS7(version string) error {
	ver := strings.Fields(version)
	if len(ver) == 0 {
		return nil
	}
	if strings.HasPrefix(ver[0], "7.") {
		return &UnsupportedError{Version: ver[0]}
	}
	return nil
}

func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	m := sizeRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid size")
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}
	mul := 1.0
	switch strings.ToLower(m[2]) {
	case "kib":
		mul = 1024
	case "mib":
		mul = 1024 * 1024
	case "gib":
		mul = 1024 * 1024 * 1024
	case "tib":
		mul = 1024 * 1024 * 1024 * 1024
	}
	return int64(n * mul), nil
}
