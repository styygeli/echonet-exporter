package metrics

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/styygeli/echonet-exporter/internal/config"
	"github.com/styygeli/echonet-exporter/internal/scraper"
	"github.com/styygeli/echonet-exporter/internal/specs"
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
	deviceInfo          *prometheus.Desc
	metricDescs         map[string]map[string]*prometheus.Desc // class -> metric name -> desc
	enumMetricDescs     map[string]map[string]map[int]*prometheus.Desc
}

// NewCollector returns a new collector that reads from the given cache.
func NewCollector(cfg *config.Config, cache *scraper.Cache, deviceSpecs map[string]*specs.DeviceSpec) *Collector {
	extraLabelKeys := collectExtraLabelKeys(cfg.Devices)
	allLabelNames := append([]string{"device", "ip", "class"}, extraLabelKeys...)
	infoLabelNames := append(append([]string{}, allLabelNames...), "manufacturer", "product_code", "uid")

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
		deviceInfo: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "device_info"),
			"Static device identity labels from generic ECHONET properties.",
			infoLabelNames,
			nil,
		),
		metricDescs:     make(map[string]map[string]*prometheus.Desc),
		enumMetricDescs: make(map[string]map[string]map[int]*prometheus.Desc),
	}

	// Build metric descriptors from specs.
	for class, spec := range deviceSpecs {
		if spec == nil {
			continue
		}
		c.metricDescs[class] = make(map[string]*prometheus.Desc)
		c.enumMetricDescs[class] = make(map[string]map[int]*prometheus.Desc)
		for _, m := range spec.Metrics {
			subsystem := subsystemForClass(class)
			c.metricDescs[class][m.Name] = prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsystem, m.Name),
				m.Help,
				allLabelNames,
				nil,
			)
			if len(m.Enum) > 0 {
				c.enumMetricDescs[class][m.Name] = buildEnumDescs(subsystem, m, allLabelNames)
			}
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

func subsystemForClass(class string) string {
	switch class {
	case "storage_battery":
		return "battery"
	case "home_solar":
		return "solar"
	case "home_ac":
		return "ac"
	default:
		return class
	}
}

func sanitizeEnumLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevUnderscore := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnderscore = false
			continue
		}
		if !prevUnderscore {
			b.WriteByte('_')
			prevUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "value"
	}
	return out
}

func buildEnumDescs(subsystem string, m specs.MetricSpec, labels []string) map[int]*prometheus.Desc {
	out := make(map[int]*prometheus.Desc, len(m.Enum))
	usedNames := make(map[string]struct{}, len(m.Enum))
	keys := make([]int, 0, len(m.Enum))
	for value := range m.Enum {
		keys = append(keys, value)
	}
	sort.Ints(keys)
	for _, value := range keys {
		label := m.Enum[value]
		suffix := sanitizeEnumLabel(label)
		metricName := fmt.Sprintf("%s_is_%s", m.Name, suffix)
		if _, exists := usedNames[metricName]; exists {
			metricName = fmt.Sprintf("%s_is_%s_0x%x", m.Name, suffix, value)
		}
		usedNames[metricName] = struct{}{}
		out[value] = prometheus.NewDesc(
			prometheus.BuildFQName(namespace, subsystem, metricName),
			fmt.Sprintf("1 if %s equals %q, else 0.", m.Name, label),
			labels,
			nil,
		)
	}
	return out
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
	ch <- c.deviceInfo
	for _, descs := range c.metricDescs {
		for _, d := range descs {
			ch <- d
		}
	}
	for _, byMetric := range c.enumMetricDescs {
		for _, byValue := range byMetric {
			for _, d := range byValue {
				ch <- d
			}
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
		info := c.cache.GetInfo(dev)
		infoLabels := append(append([]string{}, labels...), info.Manufacturer, info.ProductCode, info.UID)
		ch <- prometheus.MustNewConstMetric(c.deviceInfo, prometheus.GaugeValue, 1, infoLabels...)

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

			enumDescs, hasEnum := c.enumMetricDescs[dev.Class][name]
			if !hasEnum {
				continue
			}
			raw := int(math.Round(mv.Value))
			for enumValue, enumDesc := range enumDescs {
				enumMetricValue := 0.0
				if raw == enumValue {
					enumMetricValue = 1
				}
				ch <- prometheus.MustNewConstMetric(enumDesc, prometheus.GaugeValue, enumMetricValue, labels...)
			}
		}
	}
}
