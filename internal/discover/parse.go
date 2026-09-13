package discover

import (
	"fmt"
	"strings"
	"unicode"
)

type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type Facts struct {
	ArchitectureName string    `json:"architecture_name"`
	BoardName        string    `json:"board_name"`
	Version          string    `json:"version"`
	FreeHDDSpace     string    `json:"free_hdd_space"`
	Packages         []Package `json:"packages"`
	RouterBoard      bool      `json:"routerboard"`
	CurrentFirmware  string    `json:"current_firmware"`
	UpgradeFirmware  string    `json:"upgrade_firmware"`
	Identity         string    `json:"identity,omitempty"`
}

func Parse(resource, packages, routerboard, identity string) (Facts, error) {
	res := parseKV(resource)
	rb := parseKV(routerboard)
	id := parseKV(identity)

	version, err := ros6Version(res["version"])
	if err != nil {
		return Facts{}, err
	}
	routerBoard, err := parseRouterBoard(rb)
	if err != nil {
		return Facts{}, err
	}

	facts := Facts{
		ArchitectureName: res["architecture-name"],
		BoardName:        firstNonEmpty(res["board-name"], rb["board-name"]),
		Version:          version,
		FreeHDDSpace:     res["free-hdd-space"],
		Packages:         parsePackages(packages),
		RouterBoard:      routerBoard,
		CurrentFirmware:  rb["current-firmware"],
		UpgradeFirmware:  rb["upgrade-firmware"],
		Identity:         id["name"],
	}
	if facts.ArchitectureName == "" {
		return Facts{}, fmt.Errorf("discover: missing architecture-name")
	}
	if facts.BoardName == "" {
		return Facts{}, fmt.Errorf("discover: missing board-name")
	}
	if facts.FreeHDDSpace == "" {
		return Facts{}, fmt.Errorf("discover: missing free-hdd-space")
	}
	if facts.RouterBoard {
		if facts.CurrentFirmware == "" {
			return Facts{}, fmt.Errorf("discover: missing current-firmware")
		}
		if facts.UpgradeFirmware == "" {
			return Facts{}, fmt.Errorf("discover: missing upgrade-firmware")
		}
	}
	if facts.Identity == "" {
		return Facts{}, fmt.Errorf("discover: missing identity")
	}
	if len(facts.Packages) == 0 {
		return Facts{}, fmt.Errorf("discover: no packages")
	}
	return facts, nil
}

// HasRouterBoard also recognizes facts written before the routerboard field
// was added. Those facts always carried both firmware values.
func (f Facts) HasRouterBoard() bool {
	return f.RouterBoard || f.CurrentFirmware != "" || f.UpgradeFirmware != ""
}

// MatchInventory requires RouterOS identity to equal the inventory device name.
func MatchInventory(facts Facts, name string) error {
	if facts.Identity == "" {
		return fmt.Errorf("discover: missing identity")
	}
	if facts.Identity != name {
		return fmt.Errorf("discover: identity %q does not match inventory name %q", facts.Identity, name)
	}
	return nil
}

func ros6Version(raw string) (string, error) {
	ver := strings.Fields(raw)
	if len(ver) == 0 {
		return "", fmt.Errorf("discover: missing version")
	}
	v := ver[0]
	if strings.HasPrefix(v, "7.") {
		return "", fmt.Errorf("discover: RouterOS 7 is not supported (%s)", v)
	}
	return v, nil
}

func parseRouterBoard(rb map[string]string) (bool, error) {
	value, present := rb["routerboard"]
	if !present {
		// Older RouterOS output and existing fixtures omit the marker. Firmware
		// values identify those as RouterBOARD devices; otherwise retain the
		// historical strict missing-firmware error below.
		return true, nil
	}

	switch strings.ToLower(value) {
	case "yes":
		return true, nil
	case "no":
		if rb["current-firmware"] != "" || rb["upgrade-firmware"] != "" {
			return false, fmt.Errorf("discover: non-RouterBOARD reports RouterBOOT firmware")
		}
		return false, nil
	default:
		return false, fmt.Errorf("discover: invalid routerboard %q", value)
	}
}

func parseKV(s string) map[string]string {
	out := make(map[string]string)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func parsePackages(s string) []Package {
	var pkgs []Package
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 || !isIndex(fields[0]) {
			continue
		}
		i := 1
		if isFlag(fields[1]) {
			i = 2
		}
		if i+1 >= len(fields) {
			continue
		}
		pkgs = append(pkgs, Package{Name: fields[i], Version: fields[i+1]})
	}
	return pkgs
}

func isIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isFlag(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
