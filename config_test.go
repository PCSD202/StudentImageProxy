package main

import (
	"runtime/debug"
	"testing"
)

func TestPodMemoryBudget(t *testing.T) {
	previous := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(previous) })
	t.Setenv("MEMORY_LIMIT_BYTES", "268435456")
	t.Setenv("CACHE_MAX_BYTES", "")
	t.Setenv("GOMEMLIMIT", "")
	c, err := loadConfig()
	if err != nil || c.cacheBytes != 128<<20 || debug.SetMemoryLimit(-1) != int64(268435456/5*4) {
		t.Fatalf("unexpected config %+v or memory limit; err=%v", c, err)
	}
	t.Setenv("CACHE_MAX_BYTES", "0")
	c, err = loadConfig()
	if err != nil || c.cacheBytes != 0 {
		t.Fatal("cannot disable cache", c, err)
	}
	debug.SetMemoryLimit(42 << 20)
	t.Setenv("GOMEMLIMIT", "42MiB")
	if _, err := loadConfig(); err != nil || debug.SetMemoryLimit(-1) != 42<<20 {
		t.Fatal("explicit GOMEMLIMIT was overridden", err)
	}
}

func TestInvalidConfig(t *testing.T) {
	for _, name := range []string{"MEMORY_LIMIT_BYTES", "CACHE_MAX_BYTES", "CACHE_TTL", "INDEX_REFRESH_INTERVAL"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "invalid")
			if _, err := loadConfig(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestPublicBaseURL(t *testing.T) {
	for _, value := range []string{"https://images.example.org", "http://localhost:8080/", ""} {
		t.Setenv("PUBLIC_BASE_URL", value)
		if _, err := loadConfig(); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{"images.example.org", "//images.example.org", "ftp://images.example.org", "https://user:pass@images.example.org", "https://images.example.org/path", "https://images.example.org?x=y", "https://images.example.org#fragment"} {
		t.Setenv("PUBLIC_BASE_URL", value)
		if _, err := loadConfig(); err == nil {
			t.Fatal("invalid public base URL accepted", value)
		}
	}
}
