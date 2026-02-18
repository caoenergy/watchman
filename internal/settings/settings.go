package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	configDirEnvKey = "CONF_DIR"
	configFilename  = "watchman.yml"
	defaultBufferKB = 64
	defaultFdSize   = 4096
	defaultFdTtl    = 300
	defaultFpSize   = 5000
	defaultFpTtl    = 5
	minBufferKB     = 4
	maxBufferKB     = 1024
	minCacheSize    = 1
	minCacheTtlSec  = 1
	maxCacheTtlSec  = 86400
)

type Settings struct {
	Watchman struct {
		PluginRoot string `yaml:"plugin-root"`
		Watcher    struct {
			Paths      []string `yaml:"paths"`
			BufferSize int      `yaml:"buffer-size-kb"`
		} `yaml:"watcher"`
		Cache struct {
			FdSize int `yaml:"fd-size"`
			FdTtl  int `yaml:"fd-ttl"`
			FpSize int `yaml:"fp-size"`
			FpTtl  int `yaml:"fp-ttl"`
		} `yaml:"cache"`
	} `yaml:"watchman"`
}

func Load() (*Settings, error) {
	data, err := os.ReadFile(getConfigPath())
	if err != nil {
		return nil, err
	}
	var s Settings
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	s.applyDefaults()
	s.normalizePaths()
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// normalizePaths 规范化监控路径：Clean 并去掉末尾 '/'，保证与 radix 前缀匹配语义一致。
func (s *Settings) normalizePaths() {
	for i, p := range s.Watchman.Watcher.Paths {
		p = filepath.Clean(p)
		for len(p) > 1 && p[len(p)-1] == '/' {
			p = p[:len(p)-1]
		}
		s.Watchman.Watcher.Paths[i] = p
	}
}

func (s *Settings) applyDefaults() {
	if s.Watchman.Watcher.BufferSize <= 0 {
		s.Watchman.Watcher.BufferSize = defaultBufferKB
	}
	if s.Watchman.Cache.FdSize <= 0 {
		s.Watchman.Cache.FdSize = defaultFdSize
	}
	if s.Watchman.Cache.FdTtl <= 0 {
		s.Watchman.Cache.FdTtl = defaultFdTtl
	}
	if s.Watchman.Cache.FpSize <= 0 {
		s.Watchman.Cache.FpSize = defaultFpSize
	}
	if s.Watchman.Cache.FpTtl <= 0 {
		s.Watchman.Cache.FpTtl = defaultFpTtl
	}
}

// Validate 校验配置合法性，Load 时自动调用。
func (s *Settings) Validate() error {
	if len(s.Watchman.Watcher.Paths) == 0 {
		return errors.New("watchman.watcher.paths cannot be empty")
	}
	seen := make(map[string]bool)
	for _, p := range s.Watchman.Watcher.Paths {
		if p == "" {
			return errors.New("watchman.watcher.paths contains empty path")
		}
		if seen[p] {
			return fmt.Errorf("watchman.watcher.paths duplicate path: %s", p)
		}
		seen[p] = true
	}
	buf := s.Watchman.Watcher.BufferSize
	if buf < minBufferKB || buf > maxBufferKB {
		return fmt.Errorf("watchman.watcher.buffer-size-kb must be between %d and %d, got %d", minBufferKB, maxBufferKB, buf)
	}
	if s.Watchman.Cache.FdSize < minCacheSize {
		return fmt.Errorf("watchman.cache.fd-size must be >= %d", minCacheSize)
	}
	if s.Watchman.Cache.FdTtl < minCacheTtlSec || s.Watchman.Cache.FdTtl > maxCacheTtlSec {
		return fmt.Errorf("watchman.cache.fd-ttl must be between %d and %d seconds", minCacheTtlSec, maxCacheTtlSec)
	}
	if s.Watchman.Cache.FpSize < minCacheSize {
		return fmt.Errorf("watchman.cache.fp-size must be >= %d", minCacheSize)
	}
	if s.Watchman.Cache.FpTtl < minCacheTtlSec || s.Watchman.Cache.FpTtl > maxCacheTtlSec {
		return fmt.Errorf("watchman.cache.fp-ttl must be between %d and %d seconds", minCacheTtlSec, maxCacheTtlSec)
	}
	return nil
}

func (s *Settings) Save() error {
	data, err := yaml.Marshal(s)
	if err != nil {
		return err
	}
	return os.WriteFile(getConfigPath(), data, 0644)
}

func getConfigPath() string {
	if dir := os.Getenv(configDirEnvKey); dir != "" {
		return filepath.Join(dir, configFilename)
	}
	return configFilename
}
