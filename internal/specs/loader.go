package specs

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const defaultScrapeInterval = time.Minute

//go:embed devices/*.yaml
var builtinDevices embed.FS

// Load reads all device specs. Built-in devices from devices/*.yaml are always
// loaded. If ECHONET_DEVICES_DIR is set, additional YAML files from that
// directory are loaded (filename without .yaml = class id).
func Load() (map[string]*DeviceSpec, error) {
	out := make(map[string]*DeviceSpec)

	// Load built-in devices
	entries, err := builtinDevices.ReadDir("devices")
	if err != nil {
		return nil, fmt.Errorf("read builtin devices: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		class := strings.TrimSuffix(e.Name(), ".yaml")
		data, err := builtinDevices.ReadFile("devices/" + e.Name())
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		spec, err := parseDeviceYAML(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		out[class] = spec
	}

	// Load from external dir if set
	if dir := os.Getenv("ECHONET_DEVICES_DIR"); dir != "" {
		ext, err := loadFromDir(dir)
		if err != nil {
			return nil, err
		}
		for k, v := range ext {
			out[k] = v
		}
	}

	return out, nil
}

func loadFromDir(dir string) (map[string]*DeviceSpec, error) {
	out := make(map[string]*DeviceSpec)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read devices dir %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		class := strings.TrimSuffix(e.Name(), ".yaml")
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		spec, err := parseDeviceYAML(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		out[class] = spec
	}
	return out, nil
}

type deviceYAML struct {
	EOJ                   []int        `yaml:"eoj"`
	Description           string       `yaml:"description"`
	DefaultScrapeInterval string       `yaml:"default_scrape_interval"`
	Metrics               []metricYAML `yaml:"metrics"`
}

type metricYAML struct {
	EPC             int     `yaml:"epc"`
	Name            string  `yaml:"name"`
	Help            string  `yaml:"help"`
	Size            int     `yaml:"size"`
	Scale           float64 `yaml:"scale"`
	Signed          bool    `yaml:"signed"`
	Invalid         *int    `yaml:"invalid"`
	Type            string  `yaml:"type"`
	ScrapeInterval  string  `yaml:"scrape_interval"`
}

func parseDeviceYAML(data []byte) (*DeviceSpec, error) {
	var raw deviceYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if len(raw.EOJ) != 3 {
		return nil, fmt.Errorf("eoj must have exactly 3 bytes, got %d", len(raw.EOJ))
	}
	devInterval := defaultScrapeInterval
	if raw.DefaultScrapeInterval != "" {
		d, err := time.ParseDuration(raw.DefaultScrapeInterval)
		if err != nil {
			return nil, fmt.Errorf("default_scrape_interval %q: %w", raw.DefaultScrapeInterval, err)
		}
		if d <= 0 {
			return nil, fmt.Errorf("default_scrape_interval must be positive, got %v", d)
		}
		devInterval = d
	}

	spec := &DeviceSpec{
		Description:           raw.Description,
		DefaultScrapeInterval: devInterval,
		Metrics:               make([]MetricSpec, 0, len(raw.Metrics)),
	}
	spec.EOJ[0] = byte(raw.EOJ[0] & 0xFF)
	spec.EOJ[1] = byte(raw.EOJ[1] & 0xFF)
	spec.EOJ[2] = byte(raw.EOJ[2] & 0xFF)

	for _, m := range raw.Metrics {
		if m.Size != 1 && m.Size != 2 && m.Size != 4 {
			return nil, fmt.Errorf("metric %s: size must be 1, 2, or 4", m.Name)
		}
		if m.Type != "gauge" && m.Type != "counter" {
			return nil, fmt.Errorf("metric %s: type must be gauge or counter", m.Name)
		}
		interval := devInterval
		if m.ScrapeInterval != "" {
			d, err := time.ParseDuration(m.ScrapeInterval)
			if err != nil {
				return nil, fmt.Errorf("metric %s scrape_interval %q: %w", m.Name, m.ScrapeInterval, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("metric %s scrape_interval must be positive", m.Name)
			}
			interval = d
		}
		ms := MetricSpec{
			EPC:              byte(m.EPC & 0xFF),
			Name:             m.Name,
			Help:             m.Help,
			Size:             m.Size,
			Scale:            m.Scale,
			Signed:           m.Signed,
			Invalid:         m.Invalid,
			Type:             m.Type,
			ScrapeInterval:   interval,
		}
		if ms.Scale == 0 {
			ms.Scale = 1
		}
		spec.Metrics = append(spec.Metrics, ms)
	}
	return spec, nil
}
