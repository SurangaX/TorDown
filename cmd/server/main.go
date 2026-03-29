package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tordown/internal/server"
	"tordown/internal/torrent"
)

func main() {
	addr := envOrDefault("TORDOWN_LISTEN_ADDR", ":443")
	downloadDir := envOrDefault("TORDOWN_DOWNLOAD_DIR", "./downloads")
	sslCert := os.Getenv("TORDOWN_SSL_CERT")
	sslKey := os.Getenv("TORDOWN_SSL_KEY")
	domain := os.Getenv("TORDOWN_DOMAIN")

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

	var redirectSrv *http.Server
	var serverErr chan error = make(chan error, 2)

	go func() {
		<-shutdownCtx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("error shutting down main server: %v", err)
		}
		if redirectSrv != nil {
			if err := redirectSrv.Shutdown(ctx); err != nil {
				log.Printf("error shutting down redirect server: %v", err)
			}
		}
	}()

	// Start server with SSL if certificates are provided
	if sslCert != "" && sslKey != "" {
		// Determine HTTPS port for redirect
		httpsHost := addr
		if !strings.HasPrefix(httpsHost, ":") {
			if host, port, err := net.SplitHostPort(httpsHost); err == nil {
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
			log.Printf("HTTP redirect server listening on :80")
			if err := redirectSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Printf("redirect server error: %v", err)
				serverErr <- err
			}
		}()

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

func httpPortFromAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return strings.TrimPrefix(addr, ":")
	}
	if host, port, err := net.SplitHostPort(addr); err == nil && host != "" {
		return port
	}
	return "443"
}
