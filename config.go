package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	// SOCKS5Config SOCKS5Config `yaml:"socks5"`
}


// type SOCKS5Config struct {
// 	Enabled bool `yaml:"enabled"`
// 	Port int `yaml:"port"`
// }
type ServerConfig struct {
	Port int `yaml:"port"`
	ReadTimeout int `yaml:"read_timeout"`
	IdleConnectionsTimeout int `yaml:"idle_connections_timeout"`
	TLSHandshakeTimeout int `yaml:"tls_handshake_timeout"`
	MaxIdleConns int `yaml:"max_idle_connections"`
	RateLimiter RateLimiterConfig `yaml:"rate_limiter"`
	BandwidthLimiter BandwidthLimiterConfig `yaml:"bandwidth_limiter"`
}

type RateLimiterConfig struct {
	RequestsPerSecond int `yaml:"requests_per_second"`
	BurstCapacity int `yaml:"burst_capacity"`
}

type BandwidthLimiterConfig struct {
	SpeedLimitMB int `yaml:"speed_limit_mb"`
	BurstCapacityMB int `yaml:"burst_capacity_mb"`
}

func LoadConfig(filename string) (*Config, error) {
	buf, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(buf, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}