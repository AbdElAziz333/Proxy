package main

import (
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

type throttledReader struct {
	r io.Reader
	limiter *rate.Limiter
	ctx context.Context
}

func newThrottledReader(ctx context.Context, r io.Reader, limiter *rate.Limiter) io.Reader {
	return &throttledReader{r: r, limiter: limiter, ctx: ctx}
}

func (t *throttledReader) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if n > 0 {
		if waitErr := t.limiter.WaitN(t.ctx, n); waitErr != nil {
			return n, waitErr
		}
	}

	return n, err
}

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
	reqLimiter *rate.Limiter
	bandwidthLimiter *rate.Limiter
}

func NewProxyHandler(c *Config) *ProxyHandler {
	// send the request using a custom HTTP client
	transport := &http.Transport{
		MaxIdleConns:        c.Server.MaxIdleConns,
		IdleConnTimeout:     time.Duration(c.Server.IdleConnectionsTimeout) * time.Second,
		TLSHandshakeTimeout: time.Duration(c.Server.TLSHandshakeTimeout) * time.Second,
	}

	const megabyte = 1024 * 1024

	limitInBytesPerSec := rate.Limit(c.Server.BandwidthLimiter.SpeedLimitMB * megabyte)

	burstInBytes := c.Server.BandwidthLimiter.BurstCapacityMB * megabyte

	// bandwidthLimiter: 1 MB/s allowed globally, burst capacity of 2 MB.
	// rate.Limit takes units/second. 1024 * 1024 = 1MB
	bandwidthLimiter := rate.NewLimiter(limitInBytesPerSec, burstInBytes)

	// Example limits (Adjust as necessary or read from Config):
	// reqLimiter: 10 requests per second allowed, with a burst capacity of 15.
	reqLimiter := rate.NewLimiter(rate.Limit(c.Server.RateLimiter.RequestsPerSecond), c.Server.RateLimiter.BurstCapacity)

	return &ProxyHandler{
		client: &http.Client{
			Transport: transport,
		},
		reqLimiter: reqLimiter,
		bandwidthLimiter: bandwidthLimiter,
	}
}

func (p *ProxyHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// request rate limiting guard
	if !p.reqLimiter.Allow() {
		w.Header().Add("Retry-After", "1")
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}

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

	go p.tunnel(context.Background(), clientConn, destConn)
}

func (p *ProxyHandler) tunnel(ctx context.Context, client, dest net.Conn) {
	// Channel to signal when a one-way copy completes
	done := make(chan struct{}, 2)

	// Throttled pipes using my custom reader wrapper
	throttledClient := newThrottledReader(ctx, client, p.bandwidthLimiter)
	throttledDest := newThrottledReader(ctx, dest, p.bandwidthLimiter)

	// pipe: client to destination
	go func() {
		_, _ = io.Copy(dest, throttledClient)
		if tcpConn, ok := dest.(*net.TCPConn); ok {
			// a reached EOF, so half-close b's write stream
			_ = tcpConn.CloseWrite()
		}

		done <- struct{}{}
	} ()

	// pipe: destination to client
	go func() {
		_, _ = io.Copy(client, throttledDest)
		if tcpConn, ok := dest.(*net.TCPConn); ok {
			// b reached EOF, so half-close a's write stream
			_ = tcpConn.CloseWrite()
		}

		done <- struct{}{}
	} ()
	
	<-done
	<-done
	_ = client.Close()
	_ = dest.Close()
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

	// Explicitly let Go manage connection longevity if requested
	if req.Close {
		w.Header().Set("Connection", "close")
	}

	// set the status code before writting the body
	w.WriteHeader(resp.StatusCode)

	// Bandwidth limiting guard for standard HTTP
	// Wrap the response body reader with our throttled reader before copying to the client writer
	throttledResponseBody := newThrottledReader(req.Context(), resp.Body, p.bandwidthLimiter)

	// copy the response body to the original client
	// Go automatically streams this. If size is unknown, Go applies chunked transfer-encoding.
	_, err = io.Copy(w, throttledResponseBody)
	if err != nil {
		log.Printf("Error copying response body: %v", err)
	}
}
