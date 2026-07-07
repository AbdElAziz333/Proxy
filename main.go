package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func bootstrapConfig(filename string) *Config {
	cfg, err := LoadConfig(filename)
	if err != nil {
		return nil
	}

	return &Config{
		Server: cfg.Server,
	}
}

func main() {
	cfg := bootstrapConfig("config.yaml")
	if cfg == nil {
		panic("couldn't load config")
	}
	
	addrStr := fmt.Sprintf(":%d", cfg.Server.Port)

	handler := NewProxyHandler(cfg)
	server := &http.Server{
		Addr:    addrStr,
		Handler: handler,
		ReadTimeout: time.Duration(cfg.Server.ReadTimeout) * time.Second,
	}

	log.Printf("Starting proxy on port %d\n", cfg.Server.Port)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
