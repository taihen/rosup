package inventory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/taihen/rosup/internal/config"
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
	for i := range devices {
		d := devices[i]
		if d.Name == "" {
			return fmt.Errorf("inventory: device name is required")
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
		names[d.Name] = struct{}{}
	}
	for i := range devices {
		d := devices[i]
		for _, dep := range d.DependsOn {
			if _, ok := names[dep]; !ok {
				return fmt.Errorf("inventory: device %q depends_on unknown name %q", d.Name, dep)
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
