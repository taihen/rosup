package preflight

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/taihen/rosup/internal/discover"
	"github.com/taihen/rosup/internal/release"
)

const diskMarginBytes int64 = 1 << 20

var sizeRe = regexp.MustCompile(`(?i)^([0-9]+(?:\.[0-9]+)?)\s*(b|kib|mib|gib|tib)?$`)

func AlreadyOnRelease(facts discover.Facts, version string) bool {
	return facts.Version != "" && facts.Version == version
}

func Check(facts discover.Facts, man release.Manifest) error {
	if err := rejectROS7(facts.Version); err != nil {
		return err
	}
	if err := rejectROS7(man.Version); err != nil {
		return err
	}
	if AlreadyOnRelease(facts, man.Version) {
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
		return fmt.Errorf("preflight: missing packages for architecture %s: %s", facts.ArchitectureName, strings.Join(missing, ", "))
	}

	free, err := parseSize(facts.FreeHDDSpace)
	if err != nil {
		return fmt.Errorf("preflight: parse free-hdd-space %q: %w", facts.FreeHDDSpace, err)
	}
	need := stagedSize + diskMarginBytes
	if free < need {
		return fmt.Errorf("preflight: not enough free disk: have %d bytes, need %d bytes (staged packages + 1 MiB)", free, need)
	}
	return nil
}

func rejectROS7(version string) error {
	ver := strings.Fields(version)
	if len(ver) == 0 {
		return nil
	}
	if strings.HasPrefix(ver[0], "7.") {
		return fmt.Errorf("preflight: RouterOS 7 is not supported (%s)", ver[0])
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
