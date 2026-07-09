package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

	go func() {
		log.Printf("Starting proxy on port %d\n", cfg.Server.Port)
		if err := server.ListenAndServe(); err != nil {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	shutdownSig := make(chan os.Signal, 1)

	signal.Notify(shutdownSig, syscall.SIGINT, syscall.SIGTERM)

	sig := <-shutdownSig
	log.Printf("Received signal %v: Initiating graceful shutdown...\n", sig)

	// Give inflight proxy connections (e.g., 15 seconds) to finish responding
	ctx, cancel := context.WithTimeout(context.Background(), 10 * time.Second)
	defer cancel()

	// this stops accepting new traffic and cleanly drains active connections
	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Proxy forced to shutdown with errors: %v\n", err)
	} else {
		log.Println("Proxy server shutdown cleanly.")
	}

	log.Println("Application fully stopped.")
}