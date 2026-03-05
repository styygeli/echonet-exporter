package specs

import "time"

// DeviceSpec defines one ECHONET device class (e.g. storage_battery).
type DeviceSpec struct {
	EOJ                   [3]byte
	Description           string
	DefaultScrapeInterval time.Duration // default for metrics without scrape_interval
	Metrics               []MetricSpec
}

// MetricSpec defines one EPC to poll and how to interpret it.
type MetricSpec struct {
	EPC            byte    `yaml:"epc"`
	Name           string  `yaml:"name"`
	Help           string  `yaml:"help"`
	Size           int     `yaml:"size"`    // 1, 2, or 4 bytes
	Scale          float64 `yaml:"scale"`   // multiplier (e.g. 0.001 for kWh)
	Signed         bool    `yaml:"signed"`  // for 2 or 4 byte values
	Invalid        *int    `yaml:"invalid"` // raw value meaning invalid (e.g. 0x7FFF)
	Type           string  `yaml:"type"`    // gauge or counter
	Enum           map[int]string
	ScrapeInterval time.Duration // parsed from scrape_interval YAML (0 = use device default)
}
