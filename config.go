package main

import (
	"fmt"
	"os"
	"runtime/debug"
	"strconv"
	"time"
)

type config struct {
	cacheBytes   uint64
	cacheTTL     time.Duration
	indexRefresh time.Duration
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
