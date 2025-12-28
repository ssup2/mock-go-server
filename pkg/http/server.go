package http

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"time"
)

type Server struct {
	ServiceName string
	Port        string
	server      *http.Server
}

func NewServer(serviceName, port string) *Server {
	return &Server{
		ServiceName: serviceName,
		Port:        port,
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/", s.rootHandler)
	mux.HandleFunc("/status/", s.statusHandler)
	mux.HandleFunc("/delay/", s.delayHandler)
	mux.HandleFunc("/headers", s.headersHandler)
	mux.HandleFunc("/large", s.largeResponseHandler)
	mux.HandleFunc("/panic", s.panicHandler)
	mux.HandleFunc("/echo", s.echoHandler)
	mux.HandleFunc("/disconnect/", s.disconnectHandler)
	mux.HandleFunc("/reset/", s.resetHandler)
	mux.HandleFunc("/health", s.healthHandler)
	mux.HandleFunc("/ready", s.readyHandler)

	s.server = &http.Server{
		Addr:    ":" + s.Port,
		Handler: s.loggingMiddleware(mux),
	}

	log.Printf("[%s] Starting HTTP server on port %s", s.ServiceName, s.Port)
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	log.Printf("[%s] Shutting down HTTP server...", s.ServiceName)
	return s.server.Shutdown(ctx)
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[%s] HTTP %s %s %s", s.ServiceName, r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
		log.Printf("[%s] HTTP %s %s completed in %v", s.ServiceName, r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"service": s.ServiceName,
		"message": "Welcome to mock server for Istio/Envoy testing",
		"http_endpoints": []string{
			"/health - Health check",
			"/ready - Readiness check",
			"/status/{code} - Return specific HTTP status code",
			"/delay/{ms} - Delay response by milliseconds",
			"/headers - Echo all request headers",
			"/large?size=1000 - Large response (size in KB)",
			"/panic - Trigger panic (crash)",
			"/echo - Echo request body",
			"/disconnect/{ms} - Server closes connection after delay",
			"/reset/{ms} - Server sends TCP RST after delay",
		},
		"grpc_methods": []string{
			"Health - Health check",
			"Ready - Readiness check",
			"Status - Return specific gRPC status code",
			"Delay - Delay response by milliseconds",
			"Headers - Echo all request headers/metadata",
			"Large - Large response (size in KB)",
			"Echo - Echo request body",
		},
	})
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": s.ServiceName,
	})
}

func (s *Server) readyHandler(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, http.StatusOK, map[string]string{
		"status":  "ready",
		"service": s.ServiceName,
	})
}

func (s *Server) statusHandler(w http.ResponseWriter, r *http.Request) {
	codeStr := r.URL.Path[len("/status/"):]
	code, err := strconv.Atoi(codeStr)
	if err != nil || code < 100 || code > 599 {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid status code. Use /status/{100-599}",
		})
		return
	}

	s.respondJSON(w, code, map[string]interface{}{
		"status_code": code,
		"service":     s.ServiceName,
		"message":     http.StatusText(code),
	})
}

func (s *Server) delayHandler(w http.ResponseWriter, r *http.Request) {
	msStr := r.URL.Path[len("/delay/"):]
	ms, err := strconv.Atoi(msStr)
	if err != nil || ms < 0 {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid delay. Use /delay/{milliseconds}",
		})
		return
	}

	time.Sleep(time.Duration(ms) * time.Millisecond)
	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"service":    s.ServiceName,
		"delayed_ms": ms,
		"message":    fmt.Sprintf("Response delayed by %dms", ms),
	})
}

func (s *Server) headersHandler(w http.ResponseWriter, r *http.Request) {
	headers := make(map[string][]string)
	for key, values := range r.Header {
		headers[key] = values
	}

	s.respondJSON(w, http.StatusOK, map[string]interface{}{
		"service": s.ServiceName,
		"headers": headers,
		"method":  r.Method,
		"url":     r.URL.String(),
		"host":    r.Host,
	})
}

func (s *Server) largeResponseHandler(w http.ResponseWriter, r *http.Request) {
	sizeStr := r.URL.Query().Get("size")
	size := 1000
	if sizeStr != "" {
		if parsed, err := strconv.Atoi(sizeStr); err == nil {
			size = parsed
		}
	}

	if size > 100*1024 {
		size = 100 * 1024
	}

	data := make([]byte, size*1024)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

func (s *Server) panicHandler(w http.ResponseWriter, r *http.Request) {
	log.Printf("[%s] Panic triggered!", s.ServiceName)
	panic("Intentional panic for testing")
}

func (s *Server) echoHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Failed to read request body",
		})
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func (s *Server) disconnectHandler(w http.ResponseWriter, r *http.Request) {
	msStr := r.URL.Path[len("/disconnect/"):]
	ms, err := strconv.Atoi(msStr)
	if err != nil || ms < 0 {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid delay. Use /disconnect/{milliseconds}",
		})
		return
	}

	log.Printf("[%s] Disconnecting client connection after %dms", s.ServiceName, ms)
	time.Sleep(time.Duration(ms) * time.Millisecond)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		s.respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Hijacking not supported",
		})
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		s.respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}
	conn.Close()
}

func (s *Server) resetHandler(w http.ResponseWriter, r *http.Request) {
	msStr := r.URL.Path[len("/reset/"):]
	ms, err := strconv.Atoi(msStr)
	if err != nil || ms < 0 {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid delay. Use /reset/{milliseconds}",
		})
		return
	}

	log.Printf("[%s] Sending TCP RST to client connection after %dms", s.ServiceName, ms)
	time.Sleep(time.Duration(ms) * time.Millisecond)

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		s.respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Hijacking not supported",
		})
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		s.respondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
		return
	}

	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetLinger(0)
	}
	conn.Close()
}

func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
