package metrics

import (
	"sort"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sty/echonet-exporter/internal/config"
	"github.com/sty/echonet-exporter/internal/scraper"
	"github.com/sty/echonet-exporter/internal/specs"
)

const namespace = "echonet"

// Collector implements prometheus.Collector and serves cached metrics from detached scrapers.
type Collector struct {
	cfg            *config.Config
	cache          *scraper.Cache
	mutex          sync.Mutex
	extraLabelKeys []string

	scrapeSuccess       *prometheus.Desc
	scrapeDuration      *prometheus.Desc
	lastScrapeTimestamp *prometheus.Desc
	metricDescs         map[string]map[string]*prometheus.Desc // class -> metric name -> desc
}

// NewCollector returns a new collector that reads from the given cache.
func NewCollector(cfg *config.Config, cache *scraper.Cache, deviceSpecs map[string]*specs.DeviceSpec) *Collector {
	extraLabelKeys := collectExtraLabelKeys(cfg.Devices)
	allLabelNames := append([]string{"device", "ip", "class"}, extraLabelKeys...)

	c := &Collector{
		cfg:            cfg,
		cache:          cache,
		extraLabelKeys: extraLabelKeys,
		scrapeSuccess: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "scrape_success"),
			"1 if the last scrape of this device succeeded, 0 otherwise.",
			allLabelNames,
			nil,
		),
		scrapeDuration: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "scrape_duration_seconds"),
			"Duration of the last scrape for this device in seconds.",
			allLabelNames,
			nil,
		),
		lastScrapeTimestamp: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "last_scrape_timestamp_seconds"),
			"Unix timestamp of the last successful scrape for this device.",
			allLabelNames,
			nil,
		),
		metricDescs: make(map[string]map[string]*prometheus.Desc),
	}

	// Build metric descriptors from specs.
	for class, spec := range deviceSpecs {
		if spec == nil {
			continue
		}
		c.metricDescs[class] = make(map[string]*prometheus.Desc)
		for _, m := range spec.Metrics {
			subsystem := class
			if class == "storage_battery" {
				subsystem = "battery"
			} else if class == "home_solar" {
				subsystem = "solar"
			} else if class == "home_ac" {
				subsystem = "ac"
			}
			c.metricDescs[class][m.Name] = prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsystem, m.Name),
				m.Help,
				allLabelNames,
				nil,
			)
		}
	}

	return c
}

func collectExtraLabelKeys(devices []config.Device) []string {
	set := make(map[string]struct{})
	for _, dev := range devices {
		for k := range dev.Labels {
			if k == "" {
				continue
			}
			set[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (c *Collector) labelValues(dev config.Device) []string {
	values := make([]string, 0, 3+len(c.extraLabelKeys))
	values = append(values, dev.Name, dev.IP, dev.Class)
	for _, k := range c.extraLabelKeys {
		values = append(values, dev.Labels[k])
	}
	return values
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.scrapeSuccess
	ch <- c.scrapeDuration
	ch <- c.lastScrapeTimestamp
	for _, descs := range c.metricDescs {
		for _, d := range descs {
			ch <- d
		}
	}
}

// Collect implements prometheus.Collector. Reads from the detached scraper cache.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	for _, dev := range c.cfg.Devices {
		success, durationSec, lastScrape, metrics := c.cache.Get(dev)
		labels := c.labelValues(dev)

		successVal := 0.0
		if success {
			successVal = 1
		}

		ch <- prometheus.MustNewConstMetric(c.scrapeSuccess, prometheus.GaugeValue, successVal, labels...)
		ch <- prometheus.MustNewConstMetric(c.scrapeDuration, prometheus.GaugeValue, durationSec, labels...)

		if success && !lastScrape.IsZero() {
			ch <- prometheus.MustNewConstMetric(c.lastScrapeTimestamp, prometheus.GaugeValue, float64(lastScrape.Unix()), labels...)
		}

		for name, mv := range metrics {
			desc, ok := c.metricDescs[dev.Class][name]
			if !ok {
				continue
			}
			vt := prometheus.GaugeValue
			if mv.Type == "counter" {
				vt = prometheus.CounterValue
			}
			ch <- prometheus.MustNewConstMetric(desc, vt, mv.Value, labels...)
		}
	}
}
