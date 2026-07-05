package main

import (
	"log"
	"net/http"
)

func main() {
	handler := NewProxyHandler()
	server := &http.Server{
		Addr:    ":8080",
		Handler: handler,
	}

	log.Println("Starting proxy on port 8080")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
