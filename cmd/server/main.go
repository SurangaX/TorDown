package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tordown/internal/server"
	"tordown/internal/telegram"
	"tordown/internal/torrent"
)

func main() {
	fmt.Println(">>> TorDown Server Starting...")
	addr := envOrDefault("TORDOWN_LISTEN_ADDR", ":443")
	downloadDir := envOrDefault("TORDOWN_DOWNLOAD_DIR", "./downloads")
	sslCert := os.Getenv("TORDOWN_SSL_CERT")
	sslKey := os.Getenv("TORDOWN_SSL_KEY")
	domain := os.Getenv("TORDOWN_DOMAIN")
	storageMode := envOrDefault("TORDOWN_STORAGE_MODE", "local")

	fmt.Printf(">>> Config: Addr=%s, Dir=%s, Mode=%s\n", addr, downloadDir, storageMode)

	tgAPIID, _ := strconv.Atoi(os.Getenv("TORDOWN_TELEGRAM_API_ID"))
	tgAPIHash := os.Getenv("TORDOWN_TELEGRAM_API_HASH")

	var tgClient *telegram.Client
	if tgAPIID != 0 && tgAPIHash != "" {
		fmt.Printf(">>> Initializing Telegram (API ID: %d)\n", tgAPIID)
		tgClient = telegram.NewClient(tgAPIID, tgAPIHash, filepath.Join(downloadDir, ".telegram"))
		
		// Ensure the Telegram client is connected with a timeout to prevent hanging
		connectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		
		if err := tgClient.EnsureConnected(connectCtx); err != nil {
			fmt.Printf(">>> Warning: Failed to connect Telegram: %v\n", err)
			tgClient = nil
		} else {
			fmt.Println(">>> Telegram Connected Successfully")
		}
	} else {
		fmt.Println(">>> Telegram API credentials not provided, skipping background auth")
	}

	mgr, err := torrent.NewManager(context.Background(), torrent.Config{
		DownloadDir:    downloadDir,
		StorageMode:    storageMode,
		TelegramClient: tgClient,
	})
	if err != nil {
		fmt.Printf(">>> ERROR: Failed to create torrent manager: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(">>> Torrent Manager Ready")

	h, err := server.NewHTTPServer(server.Config{
		Manager:        mgr,
		TelegramClient: tgClient,
		StaticDir:      "web",
		DownloadDir:    downloadDir,
	})
	if err != nil {
		fmt.Printf(">>> ERROR: Failed to create http server: %v\n", err)
		os.Exit(1)
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

	var redirectSrv *http.Server
	var serverErr chan error = make(chan error, 2)

	go func() {
		<-shutdownCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			fmt.Printf(">>> Error shutting down main server: %v\n", err)
		}
		if redirectSrv != nil {
			if err := redirectSrv.Shutdown(ctx); err != nil {
				fmt.Printf(">>> Error shutting down redirect server: %v\n", err)
			}
		}
	}()

	// Start server with SSL if certificates are provided
	if sslCert != "" && sslKey != "" {
		// Determine HTTPS port for redirect
		httpsHost := addr
		if !strings.HasPrefix(httpsHost, ":") {
			if _, port, err := net.SplitHostPort(httpsHost); err == nil {
				httpsHost = ":" + port
			}
		}
		httpsPort := httpPortFromAddr(httpsHost)

		// Start HTTP redirect server on port 80
		redirectHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			target := "https://"
			if domain != "" {
				target += domain
			} else {
				// Use request host if domain not specified
				target += r.Host
			}
			if httpsPort != "443" {
				target += ":" + httpsPort
			}
			target += r.URL.Path
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		})

		redirectSrv = &http.Server{
			Addr:        ":80",
			Handler:     redirectHandler,
			ReadTimeout: 5 * time.Second,
			IdleTimeout: 60 * time.Second,
		}

		go func() {
			fmt.Printf(">>> HTTP redirect server listening on :80\n")
			if err := redirectSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				fmt.Printf(">>> Redirect server error: %v\n", err)
				serverErr <- err
			}
		}()

		fmt.Printf(">>> Torrent web UI listening on %s (HTTPS)\n", addr)
		if err := srv.ListenAndServeTLS(sslCert, sslKey); err != nil && err != http.ErrServerClosed {
			fmt.Printf(">>> ERROR: HTTPS server error: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Printf(">>> Torrent web UI listening on %s (HTTP)\n", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf(">>> ERROR: HTTP server error: %v\n", err)
			os.Exit(1)
		}
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func httpPortFromAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return strings.TrimPrefix(addr, ":")
	}
	if host, port, err := net.SplitHostPort(addr); err == nil && host != "" {
		return port
	}
	return "443"
}
