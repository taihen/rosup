package inventory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/taihen/rosup/internal/config"
	"github.com/taihen/rosup/internal/rosname"
	"gopkg.in/yaml.v3"
)

const defaultInventoryFile = "inventory/devices.yaml"

var allowedKinds = map[string]struct{}{
	"ospf":   {},
	"pppoe":  {},
	"radio":  {},
	"switch": {},
	"access": {},
}

type Device struct {
	Name              string   `yaml:"name"`
	Address           string   `yaml:"address"`
	Role              string   `yaml:"role"`
	Group             string   `yaml:"group"`
	Order             int      `yaml:"order"`
	ValidationProfile string   `yaml:"validation_profile"`
	Port              int      `yaml:"port"`
	DependsOn         []string `yaml:"depends_on"`
}

type fileInventory struct {
	Devices []Device `yaml:"devices"`
}

func Load(cfg *config.Config) ([]Device, error) {
	path := inventoryPath(cfg)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("inventory: read %s: %w", path, err)
	}
	var raw fileInventory
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("inventory: parse %s: %w", path, err)
	}
	if err := validate(raw.Devices); err != nil {
		return nil, err
	}
	sortDevices(raw.Devices)
	return raw.Devices, nil
}

func inventoryPath(cfg *config.Config) string {
	file := cfg.Ops.InventoryFile
	if file == "" {
		file = defaultInventoryFile
	}
	return filepath.Join(cfg.Ops.Path, file)
}

func validate(devices []Device) error {
	names := make(map[string]struct{}, len(devices))
	addrs := make(map[string]string, len(devices))
	for i := range devices {
		d := devices[i]
		if err := rosname.Check("inventory: device name", d.Name); err != nil {
			return err
		}
		if _, dup := names[d.Name]; dup {
			return fmt.Errorf("inventory: duplicate device name %q", d.Name)
		}
		if d.Address == "" {
			return fmt.Errorf("inventory: device %q address is required", d.Name)
		}
		if _, ok := allowedKinds[d.Role]; !ok {
			return fmt.Errorf("inventory: unknown role %q", d.Role)
		}
		if _, ok := allowedKinds[d.ValidationProfile]; !ok {
			return fmt.Errorf("inventory: unknown validation_profile %q", d.ValidationProfile)
		}
		port := d.Port
		endpoint := d.Address + ":" + strconv.Itoa(port)
		if prev, dup := addrs[endpoint]; dup {
			return fmt.Errorf("inventory: devices %q and %q share address %s", prev, d.Name, endpoint)
		}
		// Port 0 means default; treat it as colliding with any explicit port on the same host.
		if port == 0 {
			for ep, prev := range addrs {
				if strings.HasPrefix(ep, d.Address+":") {
					return fmt.Errorf("inventory: devices %q and %q share address %s", prev, d.Name, d.Address)
				}
			}
		} else if prev, ok := addrs[d.Address+":0"]; ok {
			return fmt.Errorf("inventory: devices %q and %q share address %s", prev, d.Name, d.Address)
		}
		names[d.Name] = struct{}{}
		addrs[endpoint] = d.Name
	}
	for i := range devices {
		d := devices[i]
		for _, dep := range d.DependsOn {
			if dep == d.Name {
				return fmt.Errorf("inventory: device %q depends_on itself", d.Name)
			}
			if _, ok := names[dep]; !ok {
				return fmt.Errorf("inventory: device %q depends_on unknown name %q", d.Name, dep)
			}
		}
	}
	if err := checkDependCycles(devices); err != nil {
		return err
	}
	return nil
}

func checkDependCycles(devices []Device) error {
	deps := make(map[string][]string, len(devices))
	for _, d := range devices {
		deps[d.Name] = append([]string(nil), d.DependsOn...)
	}
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[string]int, len(devices))
	var visit func(string) error
	visit = func(n string) error {
		color[n] = gray
		for _, dep := range deps[n] {
			switch color[dep] {
			case gray:
				return fmt.Errorf("inventory: depends_on cycle involving %q", n)
			case white:
				if err := visit(dep); err != nil {
					return err
				}
			}
		}
		color[n] = black
		return nil
	}
	for _, d := range devices {
		if color[d.Name] == white {
			if err := visit(d.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

func sortDevices(devices []Device) {
	sort.Slice(devices, func(i, j int) bool {
		a, b := devices[i], devices[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Order != b.Order {
			return a.Order < b.Order
		}
		return a.Name < b.Name
	})
}
