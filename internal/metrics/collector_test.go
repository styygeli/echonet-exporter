package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/styygeli/echonet-exporter/internal/config"
	"github.com/styygeli/echonet-exporter/internal/echonet"
	"github.com/styygeli/echonet-exporter/internal/scraper"
	"github.com/styygeli/echonet-exporter/internal/specs"
)

func TestCollectorEmitsDeviceInfoAndEnumMetrics(t *testing.T) {
	dev := config.Device{
		Name:   "ac1",
		IP:     "192.168.1.20",
		Class:  "home_ac",
		Labels: map[string]string{"site": "home"},
	}
	cfg := &config.Config{
		ListenAddr:       ":9191",
		ScrapeTimeoutSec: 15,
		Devices:          []config.Device{dev},
	}
	deviceSpecs := map[string]*specs.DeviceSpec{
		"home_ac": {
			Metrics: []specs.MetricSpec{
				{
					EPC:   0xB0,
					Name:  "operation_mode",
					Help:  "Operation mode",
					Size:  1,
					Scale: 1,
					Type:  "gauge",
					Enum: map[int]string{
						0x41: "auto",
						0x42: "cool",
					},
				},
			},
		},
	}

	cache := scraper.NewCache()
	cache.Update(dev, "1m", time.Minute, true, 0.1, map[string]echonet.MetricValue{
		"operation_mode": {Value: 0x42, Type: "gauge"},
	})
	cache.UpdateInfo(dev, echonet.DeviceInfo{
		UID:          "aabbcc",
		Manufacturer: "Sungrow",
		ProductCode:  "GZ-000900",
	})

	collector := NewCollector(cfg, cache, deviceSpecs)
	reg := prometheus.NewRegistry()
	if err := reg.Register(collector); err != nil {
		t.Fatalf("register collector: %v", err)
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	coolVal := metricValueByName(mfs, "echonet_ac_operation_mode_is_cool")
	autoVal := metricValueByName(mfs, "echonet_ac_operation_mode_is_auto")
	if coolVal != 1 {
		t.Fatalf("expected cool one-hot metric to be 1, got %v", coolVal)
	}
	if autoVal != 0 {
		t.Fatalf("expected auto one-hot metric to be 0, got %v", autoVal)
	}
	if metricValueByName(mfs, "echonet_device_info") != 1 {
		t.Fatalf("expected echonet_device_info metric to be 1")
	}
}

func metricValueByName(mfs []*dto.MetricFamily, name string) float64 {
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		if len(mf.GetMetric()) == 0 {
			return 0
		}
		m := mf.GetMetric()[0]
		if m.GetGauge() != nil {
			return m.GetGauge().GetValue()
		}
		if m.GetCounter() != nil {
			return m.GetCounter().GetValue()
		}
	}
	return 0
}
