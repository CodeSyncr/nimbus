package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/CodeSyncr/nimbus/plugins/captcha/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8088"
	}

	addr := ":" + port

	apiKeys := map[string]float64{
		"nc_live_default_key": 500.0,
		"test_client_key":     100.0,
	}

	if customKey := os.Getenv("NIMBUS_CLOUD_API_KEY"); customKey != "" {
		apiKeys[customKey] = 1000.0
	}

	srv := server.NewServer(&server.ServerConfig{
		Addr:    addr,
		APIKeys: apiKeys,
	})

	if err := srv.Start(); err != nil {
		log.Fatalf("Failed to start Captcha Server: %v", err)
	}

	fmt.Printf("⚡ Nimbus Captcha Server listening on %s\n", srv.Addr())

	// Graceful shutdown on SIGINT / SIGTERM
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	fmt.Println("\nShutting down Nimbus Captcha Server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	fmt.Println("Server gracefully stopped.")
}
