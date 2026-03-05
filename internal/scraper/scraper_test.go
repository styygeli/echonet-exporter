package scraper

import (
	"testing"
	"time"

	"github.com/styygeli/echonet-exporter/internal/config"
	"github.com/styygeli/echonet-exporter/internal/echonet"
	"github.com/styygeli/echonet-exporter/internal/specs"
)

func TestCacheAggregation(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test", IP: "127.0.0.1", Class: "test_class"}

	// Initially empty
	success, _, _, metrics := c.Get(dev)
	if success {
		t.Error("expected empty cache to return success=false")
	}
	if metrics != nil {
		t.Error("expected empty cache to return nil metrics")
	}

	// Group 1 succeeds
	c.Update(dev, "1m", time.Minute, true, 1.0, map[string]echonet.MetricValue{
		"m1": {Value: 10, Type: "gauge"},
	})

	success, _, _, metrics = c.Get(dev)
	if !success {
		t.Error("expected success=true after successful update")
	}
	if len(metrics) != 1 || metrics["m1"].Value != 10 {
		t.Errorf("expected m1=10, got %v", metrics)
	}

	// Group 2 fails (but Group 1 is still fresh)
	c.Update(dev, "5m", 5*time.Minute, false, 2.0, nil)

	success, _, _, metrics = c.Get(dev)
	if !success {
		t.Error("expected success=true because group 1 is still fresh")
	}
	if len(metrics) != 1 {
		t.Errorf("expected metrics to remain intact, got %v", metrics)
	}

	// Group 2 succeeds, merging metrics
	c.Update(dev, "5m", 5*time.Minute, true, 1.5, map[string]echonet.MetricValue{
		"m2": {Value: 20, Type: "counter"},
	})

	success, _, _, metrics = c.Get(dev)
	if !success {
		t.Error("expected success=true")
	}
	if len(metrics) != 2 || metrics["m2"].Value != 20 {
		t.Errorf("expected m1 and m2, got %v", metrics)
	}
}

func TestCacheFreshness(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test", IP: "127.0.0.1", Class: "test_class"}

	// Update with a very short interval
	shortInterval := 10 * time.Millisecond
	c.Update(dev, "short", shortInterval, true, 0.1, map[string]echonet.MetricValue{
		"m1": {Value: 1, Type: "gauge"},
	})

	// Immediately it should be successful
	success, _, _, _ := c.Get(dev)
	if !success {
		t.Error("expected success=true immediately after update")
	}

	// Wait for TTL to expire (TTL is max(interval*2, 5s), so we need to wait > 5s to test expiration)
	// To make the test fast, let's manually manipulate the lastAttempt time
	c.mu.Lock()
	dc := c.metrics[deviceKey(dev)]
	gs := dc.groups["short"]
	gs.lastAttempt = time.Now().Add(-10 * time.Second) // push it back in time
	dc.groups["short"] = gs
	c.metrics[deviceKey(dev)] = dc
	c.mu.Unlock()

	// Now it should be stale
	success, _, _, _ = c.Get(dev)
	if success {
		t.Error("expected success=false after TTL expired")
	}
}

func TestCacheDeviceInfo(t *testing.T) {
	c := NewCache()
	dev := config.Device{Name: "test", IP: "127.0.0.1", Class: "test_class"}

	info := echonet.DeviceInfo{
		UID:          "aabbccdd",
		Manufacturer: "Sungrow",
		ProductCode:  "GZ-000900",
	}
	c.UpdateInfo(dev, info)

	got := c.GetInfo(dev)
	if got.UID != info.UID || got.Manufacturer != info.Manufacturer || got.ProductCode != info.ProductCode {
		t.Fatalf("unexpected device info: got %+v want %+v", got, info)
	}
}

func TestFilterMetricsByReadableMap(t *testing.T) {
	metrics := []specs.MetricSpec{
		{Name: "status", EPC: 0x80},
		{Name: "mode", EPC: 0xB0},
		{Name: "temp", EPC: 0xBB},
	}
	readable := map[byte]struct{}{
		0x80: {},
		0xBB: {},
	}

	filtered, unsupported := filterMetricsByReadableMap(metrics, readable)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered metrics, got %d", len(filtered))
	}
	if filtered[0].EPC != 0x80 || filtered[1].EPC != 0xBB {
		t.Fatalf("unexpected filtered metrics: %+v", filtered)
	}
	if len(unsupported) != 1 || unsupported[0] != 0xB0 {
		t.Fatalf("unexpected unsupported EPCs: %+v", unsupported)
	}
}
