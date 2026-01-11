package http

import (
	"context"
	"encoding/json"
	"fmt"
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
	mux.HandleFunc("/close-before-response/", s.closeBeforeResponseHandler)
	mux.HandleFunc("/close-after-response/", s.closeAfterResponseHandler)
	mux.HandleFunc("/wrongprotocol/", s.wrongprotocolHandler)
	mux.HandleFunc("/reset-before-response/", s.resetBeforeResponseHandler)
	mux.HandleFunc("/reset-after-response/", s.resetAfterResponseHandler)

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
			"/status/{code} - Return specific HTTP status code",
			"/delay/{ms} - Delay response by milliseconds",
			"/close-before-response/{ms} - Server closes connection before response",
			"/close-after-response/{ms} - Server sends dummy data, then closes connection",
			"/wrongprotocol/{ms} - Server sends wrong protocol data after delay",
			"/reset-before-response/{ms} - Server sends TCP RST before response",
			"/reset-after-response/{ms} - Server sends dummy data, then TCP RST",
		},
		"grpc_methods": []string{
			"Status - Return specific gRPC status code",
			"Delay - Delay response by milliseconds",
			"CloseBeforeResponse - Server closes connection before response",
			"CloseAfterResponse - Server sends dummy data, then closes connection",
			"WrongProtocol - Server sends wrong protocol data after delay",
			"ResetBeforeResponse - Server sends TCP RST before response",
			"ResetAfterResponse - Server sends dummy data, then TCP RST",
		},
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

func (s *Server) closeBeforeResponseHandler(w http.ResponseWriter, r *http.Request) {
	msStr := r.URL.Path[len("/close-before-response/"):]
	ms, err := strconv.Atoi(msStr)
	if err != nil || ms < 0 {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid delay. Use /close-before-response/{milliseconds}",
		})
		return
	}

	log.Printf("[%s] Closing connection before response after %dms", s.ServiceName, ms)
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

func (s *Server) closeAfterResponseHandler(w http.ResponseWriter, r *http.Request) {
	msStr := r.URL.Path[len("/close-after-response/"):]
	ms, err := strconv.Atoi(msStr)
	if err != nil || ms < 0 {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid delay. Use /close-after-response/{milliseconds}",
		})
		return
	}

	log.Printf("[%s] Sending dummy data after %dms, then closing connection", s.ServiceName, ms)
	time.Sleep(time.Duration(ms) * time.Millisecond)

	// Send response headers and body first
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	dummyData := []byte("dummy data")
	if _, err := w.Write(dummyData); err != nil {
		log.Printf("[%s] Failed to write dummy data: %v", s.ServiceName, err)
		return
	}

	// Flush response data
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	time.Sleep(100 * time.Millisecond)

	// Then hijack and close
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hijacker.Hijack()
	if err != nil {
		return
	}
	conn.Close()
}

func (s *Server) wrongprotocolHandler(w http.ResponseWriter, r *http.Request) {
	msStr := r.URL.Path[len("/wrongprotocol/"):]
	ms, err := strconv.Atoi(msStr)
	if err != nil || ms < 0 {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid delay. Use /wrongprotocol/{milliseconds}",
		})
		return
	}

	log.Printf("[%s] Sending wrong protocol data to client connection after %dms", s.ServiceName, ms)
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

	// Write wrong protocol data before closing
	dummyData := []byte("WRONG_PROTOCOL_DATA\n")
	if _, err := conn.Write(dummyData); err != nil {
		log.Printf("[%s] Failed to write wrong protocol data: %v", s.ServiceName, err)
	} else {
		log.Printf("[%s] Wrote wrong protocol data", s.ServiceName)
	}
	conn.Close()
}

func (s *Server) resetBeforeResponseHandler(w http.ResponseWriter, r *http.Request) {
	msStr := r.URL.Path[len("/reset-before-response/"):]
	ms, err := strconv.Atoi(msStr)
	if err != nil || ms < 0 {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid delay. Use /reset-before-response/{milliseconds}",
		})
		return
	}

	log.Printf("[%s] Sending TCP RST before response after %dms", s.ServiceName, ms)
	time.Sleep(time.Duration(ms) * time.Millisecond)

	// Don't read the request body - hijack immediately
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

	// Set SO_LINGER to 0 and close connection to force RST
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if err := tcpConn.SetLinger(0); err != nil {
			log.Printf("[%s] Failed to set SO_LINGER to 0: %v", s.ServiceName, err)
		} else {
			log.Printf("[%s] Set SO_LINGER to 0 successfully, closing connection to force RST", s.ServiceName)
		}
	} else {
		log.Printf("[%s] Connection is not a TCP connection, cannot set SO_LINGER", s.ServiceName)
	}
	conn.Close()
}

func (s *Server) resetAfterResponseHandler(w http.ResponseWriter, r *http.Request) {
	msStr := r.URL.Path[len("/reset-after-response/"):]
	ms, err := strconv.Atoi(msStr)
	if err != nil || ms < 0 {
		s.respondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Invalid delay. Use /reset-after-response/{milliseconds}",
		})
		return
	}

	log.Printf("[%s] Sending response first, then TCP RST after %dms", s.ServiceName, ms)
	time.Sleep(time.Duration(ms) * time.Millisecond)

	// Send response headers and body first
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)

	dummyData := []byte("dummy data")
	if _, err := w.Write(dummyData); err != nil {
		log.Printf("[%s] Failed to write dummy data: %v", s.ServiceName, err)
		return
	}

	// Flush response data to ensure it's sent before hijacking
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
		log.Printf("[%s] Flushed response data", s.ServiceName)
	} else {
		log.Printf("[%s] ResponseWriter does not support Flusher interface", s.ServiceName)
	}

	time.Sleep(10 * time.Millisecond)

	// Then hijack and send RST
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

	// Set SO_LINGER to 0 and close connection to force RST
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if err := tcpConn.SetLinger(0); err != nil {
			log.Printf("[%s] Failed to set SO_LINGER to 0: %v", s.ServiceName, err)
		} else {
			log.Printf("[%s] Set SO_LINGER to 0 successfully, closing connection to force RST", s.ServiceName)
		}
	} else {
		log.Printf("[%s] Connection is not a TCP connection, cannot set SO_LINGER", s.ServiceName)
	}
	conn.Close()
}

func (s *Server) respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
