package main

import (
	"fmt"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"time"
)

type config struct {
	cacheBytes    uint64
	cacheTTL      time.Duration
	indexRefresh  time.Duration
	publicBaseURL string
}

func loadConfig() (config, error) {
	budget := uint64(64 << 20)
	if raw := os.Getenv("MEMORY_LIMIT_BYTES"); raw != "" {
		limit, err := strconv.ParseUint(raw, 10, 63)
		if err != nil || limit == 0 {
			return config{}, fmt.Errorf("MEMORY_LIMIT_BYTES must be a positive byte count")
		}
		budget = limit / 2
		if os.Getenv("GOMEMLIMIT") == "" {
			debug.SetMemoryLimit(int64(limit / 5 * 4))
		}
	}
	if raw := os.Getenv("CACHE_MAX_BYTES"); raw != "" {
		value, err := strconv.ParseUint(raw, 10, 63)
		if err != nil {
			return config{}, fmt.Errorf("CACHE_MAX_BYTES must be a nonnegative byte count")
		}
		budget = value
	}
	c := config{cacheBytes: budget, cacheTTL: 24 * time.Hour, indexRefresh: 5 * time.Minute}
	c.publicBaseURL = os.Getenv("PUBLIC_BASE_URL")
	if c.publicBaseURL != "" {
		base, err := url.Parse(c.publicBaseURL)
		if err != nil || (base.Scheme != "https" && base.Scheme != "http") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.ForceQuery || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
			return config{}, fmt.Errorf("PUBLIC_BASE_URL must be an HTTP(S) origin such as https://images.example.org")
		}
		c.publicBaseURL = base.Scheme + "://" + base.Host
	}
	for name, value := range map[string]*time.Duration{
		"CACHE_TTL": &c.cacheTTL, "INDEX_REFRESH_INTERVAL": &c.indexRefresh,
	} {
		if raw := os.Getenv(name); raw != "" {
			duration, err := time.ParseDuration(raw)
			if err != nil || duration <= 0 {
				return config{}, fmt.Errorf("%s must be a positive duration such as 5m or 24h", name)
			}
			*value = duration
		}
	}
	return c, nil
}
