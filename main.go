package main

import (
	"io"
	"log"
	"net/http"
	"time"
)

type ProxyHandler struct {}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Validate the request URL
	if req.URL.Host == "" {
		http.Error(w, "bad request: missing host", http.StatusBadRequest)
		return
	}

	log.Printf("Proxying request for: %s %s", req.Method, req.URL.String())

	// create a new request to forward to the destination server
	// we use req.Context() to pass through cancellation signals
	proxyReq, err := http.NewRequestWithContext(req.Context(), req.Method, req.URL.String(), req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// copy the headers from the original request to the proxy request
	for key, values := range req.Header	 {
		for _, value := range values {
			proxyReq.Header.Add(key, value)
		}
	}

	// send the request using a custom HTTP client
	transport := &http.Transport{
		MaxIdleConns: 100,
		IdleConnTimeout: 90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	client := &http.Client{Transport: transport}

	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	defer resp.Body.Close()

	// copy the response headers back to the original client
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// set the status code before writting the body
	w.WriteHeader(resp.StatusCode)

	// copy the response body to the original client
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		log.Printf("Error copying response body: %v", err)
	}
}

func main() {
	handler := &ProxyHandler{}
	server := &http.Server{
		Addr: ":8080",
		Handler: handler,
	}

	log.Println("Starting proxy on port 8080")

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}