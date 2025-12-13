package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	grpcserver "mock-go-server/pkg/grpc"
	httpserver "mock-go-server/pkg/http"
)

var (
	serviceName = getEnv("SERVICE_NAME", "mock-server")
	httpPort    = getEnv("HTTP_PORT", "8080")
	grpcPort    = getEnv("GRPC_PORT", "9090")
)

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func main() {
	// Start HTTP server
	http := httpserver.NewServer(serviceName, httpPort)
	go func() {
		if err := http.Start(); err != nil && err.Error() != "http: Server closed" {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Start gRPC server
	grpc := grpcserver.NewServer(serviceName, grpcPort)
	go func() {
		if err := grpc.Start(); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Printf("[%s] Received shutdown signal", serviceName)

	// Graceful shutdown with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	// Shutdown HTTP server
	go func() {
		defer wg.Done()
		if err := http.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	// Shutdown gRPC server
	go func() {
		defer wg.Done()
		if err := grpc.GracefulStop(ctx); err != nil {
			log.Printf("gRPC server shutdown error: %v", err)
		}
	}()

	wg.Wait()
	log.Printf("[%s] Server stopped", serviceName)
}
