package scraper

import (
	"testing"
	"time"

	"github.com/sty/echonet-exporter/internal/config"
	"github.com/sty/echonet-exporter/internal/echonet"
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
