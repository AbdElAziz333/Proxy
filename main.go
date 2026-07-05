package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

func removeHopByHopHeaders(h http.Header) {
	// because Connection header can declaree additional hop-by-hop headers
	if c := h.Get("Connection"); c != "" {
		for _, f := range strings.Split(c, ",") {
			h.Del(strings.TrimSpace(f))
		}
	}

	h.Del("Connection")
	h.Del("Proxy-Connection")
	h.Del("Keep-Alive")
	h.Del("Proxy-Authenticate")
	h.Del("Proxy-Authorization")
	h.Del("TE")
	h.Del("Trailer")
	h.Del("Transfer-Encoding")
	h.Del("Upgrade")
}

type ProxyHandler struct {
	client *http.Client
}

func NewProxyHandler() *ProxyHandler {
	// send the request using a custom HTTP client
	transport := &http.Transport{
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return &ProxyHandler{
		client: &http.Client{
			Transport: transport,
		},
	}
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Method == http.MethodConnect {
		p.handleConnect(w, req)
		return
	}

	p.handleHTTP(w, req)
}

func (p *ProxyHandler) handleConnect(w http.ResponseWriter, req *http.Request) {
	log.Printf("CONNECT %s", req.Host)

	destConn, err := net.DialTimeout("tcp", req.Host, 10*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		destConn.Close()
		http.Error(w, "Hijacking not supported", http.StatusBadRequest)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		destConn.Close()
		return
	}

	_, err = clientConn.Write([]byte(
		"HTTP/1.1 200 Connection Established\r\n\r\n",
	))

	if err != nil {
		clientConn.Close()
		destConn.Close()
		return
	}

	go transfer(destConn, clientConn)
	go transfer(clientConn, destConn)
}

func transfer(dst io.WriteCloser, src io.ReadCloser) {
	defer dst.Close()
	defer src.Close()

	io.Copy(dst, src)
}

func (p *ProxyHandler) handleHTTP(w http.ResponseWriter, req *http.Request) {
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
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// copy the headers from the original request to the proxy request
	proxyReq.Header = req.Header.Clone()

	// remov hop-by-hop headers
	removeHopByHopHeaders(proxyReq.Header)

	resp, err := p.client.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	defer resp.Body.Close()

	removeHopByHopHeaders(resp.Header)

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
