package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	SOCKS5Config SOCKS5Config `yaml:"socks5"`
}


type SOCKS5Config struct {
	Enabled bool `yaml:"enabled"`
	Port int `yaml:"port"`
}
type ServerConfig struct {
	Port int `yaml:"port"`
	ReadTimeout int `yaml:"read_timeout"`
	IdleConnectionsTimeout int `yaml:"idle_connections_timeout"`
	TLSHandshakeTimeout int `yaml:"tls_handshake_timeout"`
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