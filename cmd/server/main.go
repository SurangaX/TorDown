package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tordown/internal/server"
	"tordown/internal/torrent"
)

func main() {
	addr := envOrDefault("TORDOWN_LISTEN_ADDR", ":8080")
	downloadDir := envOrDefault("TORDOWN_DOWNLOAD_DIR", "./downloads")
	sslCert := os.Getenv("TORDOWN_SSL_CERT")
	sslKey := os.Getenv("TORDOWN_SSL_KEY")

	mgr, err := torrent.NewManager(context.Background(), torrent.Config{
		DownloadDir: downloadDir,
	})
	if err != nil {
		log.Fatalf("failed to initialize torrent manager: %v", err)
	}
	defer mgr.Close()

	h, err := server.NewHTTPServer(server.Config{
		Manager:     mgr,
		StaticDir:   "web",
		DownloadDir: downloadDir,
	})
	if err != nil {
		log.Fatalf("failed to create http server: %v", err)
	}

	srv := &http.Server{
		Addr:         addr,
		Handler:      h,
		ReadTimeout:  15 * time.Second,
		// Keep write timeout disabled to allow long-running download streams.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-shutdownCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("error shutting down server: %v", err)
		}
	}()

	// Start server with SSL if certificates are provided
	if sslCert != "" && sslKey != "" {
		log.Printf("torrent web UI listening on %s (HTTPS)", addr)
		if err := srv.ListenAndServeTLS(sslCert, sslKey); err != nil && err != http.ErrServerClosed {
			log.Fatalf("https server error: %v", err)
		}
	} else {
		log.Printf("torrent web UI listening on %s (HTTP)", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
