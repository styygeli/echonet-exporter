package specs

import (
	"strings"
	"testing"
	"time"
)

func TestParseDeviceYAML(t *testing.T) {
	yamlData := []byte(`
eoj: [0x01, 0x30, 0x01]
description: "Test device"
default_scrape_interval: 2m

metrics:
  - epc: 0x80
    name: op_status
    help: "Status"
    size: 1
    scale: 1
    type: gauge
  - epc: 0xE0
    name: slow_metric
    help: "Slow"
    size: 4
    scale: 0.1
    signed: true
    type: counter
    scrape_interval: 10m
  - epc: 0xB0
    name: operation_mode
    size: 1
    type: gauge
    enum:
      0x41: auto
      0x42: cool
`)

	spec, err := parseDeviceYAML(yamlData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if spec.EOJ[0] != 0x01 || spec.EOJ[1] != 0x30 || spec.EOJ[2] != 0x01 {
		t.Errorf("wrong EOJ: %v", spec.EOJ)
	}

	if spec.DefaultScrapeInterval != 2*time.Minute {
		t.Errorf("wrong default interval: %v", spec.DefaultScrapeInterval)
	}

	if len(spec.Metrics) != 3 {
		t.Fatalf("expected 3 metrics, got %d", len(spec.Metrics))
	}

	m1 := spec.Metrics[0]
	if m1.EPC != 0x80 || m1.Name != "op_status" || m1.Size != 1 || m1.Type != "gauge" {
		t.Errorf("wrong m1: %+v", m1)
	}
	if m1.ScrapeInterval != 2*time.Minute {
		t.Errorf("m1 should inherit default interval, got %v", m1.ScrapeInterval)
	}

	m2 := spec.Metrics[1]
	if m2.EPC != 0xE0 || m2.Scale != 0.1 || !m2.Signed || m2.Type != "counter" {
		t.Errorf("wrong m2: %+v", m2)
	}
	if m2.ScrapeInterval != 10*time.Minute {
		t.Errorf("m2 should have 10m interval, got %v", m2.ScrapeInterval)
	}

	m3 := spec.Metrics[2]
	if m3.EPC != 0xB0 || m3.Help != "Operation mode setting" {
		t.Errorf("wrong m3 help fallback: %+v", m3)
	}
	if len(m3.Enum) != 2 || m3.Enum[0x41] != "auto" || m3.Enum[0x42] != "cool" {
		t.Errorf("wrong m3 enum mapping: %+v", m3.Enum)
	}
}

func TestParseDeviceYAML_Errors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "invalid eoj size",
			yaml: `eoj: [0x01, 0x30]`,
			want: "eoj must have exactly 3 bytes",
		},
		{
			name: "invalid eoj value",
			yaml: `eoj: [0x01, 0x30, 300]`,
			want: "eoj[2] must be in range 0..255",
		},
		{
			name: "invalid epc value",
			yaml: `
eoj: [0x01, 0x30, 0x01]
metrics:
  - epc: 300
    name: test
    size: 1
    type: gauge
`,
			want: "epc must be in range 0..255",
		},
		{
			name: "invalid size",
			yaml: `
eoj: [0x01, 0x30, 0x01]
metrics:
  - epc: 0x80
    name: test
    size: 3
    type: gauge
`,
			want: "size must be 1, 2, or 4",
		},
		{
			name: "invalid type",
			yaml: `
eoj: [0x01, 0x30, 0x01]
metrics:
  - epc: 0x80
    name: test
    size: 1
    type: histogram
`,
			want: "type must be gauge or counter",
		},
		{
			name: "enum requires scale 1",
			yaml: `
eoj: [0x01, 0x30, 0x01]
metrics:
  - epc: 0xB0
    name: mode
    size: 1
    scale: 0.1
    type: gauge
    enum:
      0x41: auto
`,
			want: "enum mapping requires scale=1",
		},
		{
			name: "enum value out of range",
			yaml: `
eoj: [0x01, 0x30, 0x01]
metrics:
  - epc: 0xB0
    name: mode
    size: 1
    type: gauge
    enum:
      300: invalid
`,
			want: "enum value 300 doesn't fit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDeviceYAML([]byte(tt.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error containing %q, got %q", tt.want, err.Error())
			}
		})
	}
}
