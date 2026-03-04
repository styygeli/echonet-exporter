package scraper

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/sty/echonet-exporter/internal/config"
	"github.com/sty/echonet-exporter/internal/echonet"
	"github.com/sty/echonet-exporter/internal/specs"
)

// Cache holds the latest scraped metrics per device. Safe for concurrent use.
type Cache struct {
	mu      sync.RWMutex
	metrics map[string]deviceCache // deviceKey -> metrics + scrape state
}

type deviceCache struct {
	success       bool
	durationSec   float64
	lastScrape    time.Time
	metrics       map[string]echonet.MetricValue
}

// deviceKey returns a unique key for a configured device.
func deviceKey(dev config.Device) string {
	return dev.Name + "|" + dev.IP + "|" + dev.Class
}

// NewCache creates an empty cache.
func NewCache() *Cache {
	return &Cache{metrics: make(map[string]deviceCache)}
}

// Get returns the cached result for a device, or nil if never scraped.
func (c *Cache) Get(dev config.Device) (success bool, durationSec float64, lastScrape time.Time, metrics map[string]echonet.MetricValue) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	dc, ok := c.metrics[deviceKey(dev)]
	if !ok {
		return false, 0, time.Time{}, nil
	}
	mcopy := make(map[string]echonet.MetricValue, len(dc.metrics))
	for k, v := range dc.metrics {
		mcopy[k] = v
	}
	return dc.success, dc.durationSec, dc.lastScrape, mcopy
}

// Update merges a scrape result into the cache for a device. Multiple scrapers
// (e.g. different intervals) can update the same device; metrics are merged.
func (c *Cache) Update(dev config.Device, success bool, durationSec float64, metrics map[string]echonet.MetricValue) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := deviceKey(dev)
	dc := c.metrics[key]
	if dc.metrics == nil {
		dc.metrics = make(map[string]echonet.MetricValue)
	}
	for k, v := range metrics {
		dc.metrics[k] = v
	}
	if success {
		dc.success = true
		dc.durationSec = durationSec
		dc.lastScrape = time.Now()
	}
	c.metrics[key] = dc
}

// Start begins background scrapers for all configured devices. Call with a context
// that is cancelled on shutdown.
func (c *Cache) Start(ctx context.Context, cfg *config.Config, deviceSpecs map[string]*specs.DeviceSpec) {
	client := echonet.NewClient(cfg.ScrapeTimeoutSec)

	for _, dev := range cfg.Devices {
		spec, ok := deviceSpecs[dev.Class]
		if !ok || spec == nil {
			log.Printf("scraper: unknown class %q for device %s, skipping", dev.Class, dev.Name)
			continue
		}

		// Per-device interval override from config
		devDefaultInterval := spec.DefaultScrapeInterval
		if dev.ScrapeInterval != "" {
			d, err := time.ParseDuration(dev.ScrapeInterval)
			if err != nil {
				log.Printf("scraper: device %s invalid scrape_interval %q: %v", dev.Name, dev.ScrapeInterval, err)
			} else if d > 0 {
				devDefaultInterval = d
			}
		}

		// Group metrics by their scrape interval
		byInterval := make(map[time.Duration][]specs.MetricSpec)
		for _, m := range spec.Metrics {
			iv := m.ScrapeInterval
			if iv == 0 {
				iv = devDefaultInterval
			}
			byInterval[iv] = append(byInterval[iv], m)
		}

		for interval, metrics := range byInterval {
			go c.runScraper(ctx, client, dev, spec, metrics, interval)
		}
	}
}

func (c *Cache) runScraper(ctx context.Context, client *echonet.Client, dev config.Device, spec *specs.DeviceSpec, metrics []specs.MetricSpec, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Scrape immediately, then on tick
	c.scrapeOnce(client, dev, spec, metrics)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.scrapeOnce(client, dev, spec, metrics)
		}
	}
}

func (c *Cache) scrapeOnce(client *echonet.Client, dev config.Device, spec *specs.DeviceSpec, metrics []specs.MetricSpec) {
	epcs := make([]byte, 0, len(metrics))
	for _, m := range metrics {
		epcs = append(epcs, m.EPC)
	}

	start := time.Now()
	raw, err := client.SendGet(dev.IP, spec.EOJ, epcs)
	durationSec := time.Since(start).Seconds()

	out := make(map[string]echonet.MetricValue)
	if err != nil {
		log.Printf("scrape %s (%s): %v", dev.Name, dev.IP, err)
		c.Update(dev, false, durationSec, out)
		return
	}

	_, props, err := echonet.ParseGetRes(raw)
	if err != nil {
		log.Printf("scrape %s (%s): parse: %v", dev.Name, dev.IP, err)
		c.Update(dev, false, durationSec, out)
		return
	}

	out = echonet.ParsePropsToMetrics(props, metrics)
	c.Update(dev, true, durationSec, out)
}
