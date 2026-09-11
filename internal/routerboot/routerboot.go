package routerboot

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

const CmdUpgrade = "/system routerboard upgrade"

func Newer(upgradeFirmware, currentFirmware string) bool {
	newer, err := Compare(upgradeFirmware, currentFirmware)
	return err == nil && newer
}

// Compare reports whether upgradeFirmware is newer than currentFirmware.
// Empty or unparseable values return an error instead of silently skipping.
func Compare(upgradeFirmware, currentFirmware string) (bool, error) {
	upToken := parseToken(upgradeFirmware)
	curToken := parseToken(currentFirmware)
	if upToken == "" {
		return false, fmt.Errorf("routerboot: empty upgrade-firmware")
	}
	if curToken == "" {
		return false, fmt.Errorf("routerboot: empty current-firmware")
	}
	if upToken == curToken {
		return false, nil
	}
	upgrade, upgradeOK := parseVersion(upgradeFirmware)
	current, currentOK := parseVersion(currentFirmware)
	if !upgradeOK {
		return false, fmt.Errorf("routerboot: unparseable upgrade-firmware %q", upgradeFirmware)
	}
	if !currentOK {
		return false, fmt.Errorf("routerboot: unparseable current-firmware %q", currentFirmware)
	}
	return compare(upgrade, current) > 0, nil
}

func ParseFirmware(routerboardPrint string) (current, upgrade string) {
	for _, line := range strings.Split(routerboardPrint, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "current-firmware":
			current = val
		case "upgrade-firmware":
			upgrade = val
		}
	}
	return current, upgrade
}

func parseToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func parseVersion(s string) ([]int, bool) {
	token := parseToken(s)
	if token == "" {
		return nil, false
	}
	parts := strings.Split(token, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, rest, ok := leadingInt(p)
		if !ok {
			return nil, false
		}
		out = append(out, n)
		if rest != "" {
			break
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

func leadingInt(s string) (int, string, bool) {
	i := 0
	for i < len(s) && unicode.IsDigit(rune(s[i])) {
		i++
	}
	if i == 0 {
		return 0, s, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, s, false
	}
	return n, s[i:], true
}

func compare(a, b []int) int {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		av, bv := 0, 0
		if i < len(a) {
			av = a[i]
		}
		if i < len(b) {
			bv = b[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}
