package server

import (
    "archive/zip"
    "context"
    "encoding/base64"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"

    "tordown/internal/torrent"
)

// Config wires dependencies for the HTTP server facade.
type Config struct {
    Manager     *torrent.Manager
    StaticDir   string
    DownloadDir string
}

// NewHTTPServer constructs the chi router responsible for API and static assets.
func NewHTTPServer(cfg Config) (http.Handler, error) {
    if cfg.Manager == nil {
        return nil, errors.New("torrent manager is required")
    }

    srv := &httpServer{
        manager:     cfg.Manager,
        downloadDir: cfg.DownloadDir,
    }

    if trimmed := strings.TrimSpace(cfg.StaticDir); trimmed != "" {
        abs, err := filepath.Abs(trimmed)
        if err != nil {
            return nil, err
        }
        srv.staticDir = abs
    }

    r := chi.NewRouter()
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(middleware.Heartbeat("/healthz"))
    r.Use(middleware.Timeout(60 * time.Second))

    r.Route("/api", srv.mountAPI)

    if srv.staticDir != "" {
        fs := http.FileServer(http.Dir(srv.staticDir))
        r.Handle("/*", srv.spaHandler(fs))
    } else {
        r.NotFound(srv.notFound)
    }

    return r, nil
}

type httpServer struct {
    manager     *torrent.Manager
    staticDir   string
    downloadDir string
}

func (s *httpServer) mountAPI(r chi.Router) {
    r.Get("/health", s.handleHealth)
    r.Get("/stats", s.handleStats)
    r.Get("/system", s.handleSystemResources)
    r.Get("/torrents", s.handleListTorrents)
    r.Post("/torrents", s.handleAddTorrent)

    r.Route("/torrents/{infoHash}", func(r chi.Router) {
        r.Get("/", s.handleGetTorrent)
        r.Delete("/", s.handleDeleteTorrent)
        r.Post("/pause", s.handlePauseTorrent)
        r.Post("/resume", s.handleResumeTorrent)
        r.Post("/verify", s.handleVerifyTorrent)
        r.Post("/selection", s.handleUpdateSelection)
        r.Get("/files/{fileIndex}", s.handleDownloadFile)
        r.Get("/download-zip", s.handleDownloadZip)
    })
}

func (s *httpServer) handleHealth(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *httpServer) handleStats(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, s.manager.Stats())
}

func (s *httpServer) handleSystemResources(w http.ResponseWriter, r *http.Request) {
    dataDir := s.downloadDir
    if dataDir == "" {
        dataDir = "."
    }
    
    resources, err := GetSystemResources(dataDir)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    
    respondJSON(w, http.StatusOK, resources)
}

func (s *httpServer) handleListTorrents(w http.ResponseWriter, r *http.Request) {
    respondJSON(w, http.StatusOK, s.manager.ListTorrents())
}

func (s *httpServer) handleGetTorrent(w http.ResponseWriter, r *http.Request) {
    infoHash := chi.URLParam(r, "infoHash")
    summary, err := s.manager.GetTorrent(r.Context(), infoHash)
    if errors.Is(err, torrent.ErrMetadataUnavailable) {
        respondJSON(w, http.StatusAccepted, summary)
        return
    }
    if err != nil {
        respondError(w, err)
        return
    }
    respondJSON(w, http.StatusOK, summary)
}

func (s *httpServer) handleAddTorrent(w http.ResponseWriter, r *http.Request) {
    defer r.Body.Close()
    var payload addTorrentRequest
    if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
        respondError(w, err)
        return
    }

    payload.MagnetURI = strings.TrimSpace(payload.MagnetURI)
    payload.TorrentFile = strings.TrimSpace(payload.TorrentFile)
    payload.TorrentURL = strings.TrimSpace(payload.TorrentURL)

    opts := torrent.AddOptions{}
    if payload.ApplySelection {
        opts.HasSelection = true
        opts.Files = append([]int(nil), payload.SelectedFiles...)
    }

    provided := 0
    if payload.MagnetURI != "" {
        provided++
    }
    if payload.TorrentFile != "" {
        provided++
    }
    if payload.TorrentURL != "" {
        provided++
    }

    if provided == 0 {
        respondError(w, errors.New("magnetUri, torrentFile, or torrentUrl is required"))
        return
    }
    if provided > 1 {
        respondError(w, errors.New("provide only one of magnetUri, torrentFile, or torrentUrl"))
        return
    }

    switch {
    case payload.MagnetURI != "":
        summary, err := s.manager.AddMagnet(r.Context(), payload.MagnetURI, opts)
        if err != nil {
            respondError(w, err)
            return
        }
        respondJSON(w, http.StatusCreated, summary)
    case payload.TorrentFile != "":
        raw, err := decodeBase64Payload(payload.TorrentFile)
        if err != nil {
            respondError(w, err)
            return
        }
        summary, err := s.manager.AddTorrentFile(r.Context(), raw, opts)
        if err != nil {
            respondError(w, err)
            return
        }
        respondJSON(w, http.StatusCreated, summary)
    case payload.TorrentURL != "":
        summary, err := s.manager.AddTorrentURL(r.Context(), payload.TorrentURL, opts)
        if err != nil {
            respondError(w, err)
            return
        }
        respondJSON(w, http.StatusCreated, summary)
    }
}

func (s *httpServer) handleUpdateSelection(w http.ResponseWriter, r *http.Request) {
    infoHash := chi.URLParam(r, "infoHash")
    defer r.Body.Close()

    var payload selectionRequest
    if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
        respondError(w, err)
        return
    }

    hasSelection := payload.ApplySelection
    files := payload.SelectedFiles
    if hasSelection && files == nil {
        files = []int{}
    }

    summary, err := s.manager.UpdateSelection(r.Context(), infoHash, files, hasSelection)
    if err != nil {
        respondError(w, err)
        return
    }

    respondJSON(w, http.StatusOK, summary)
}

func (s *httpServer) handleDeleteTorrent(w http.ResponseWriter, r *http.Request) {
    infoHash := chi.URLParam(r, "infoHash")
    deleteData, _ := strconv.ParseBool(r.URL.Query().Get("deleteData"))
    if err := s.manager.RemoveTorrent(infoHash, deleteData); err != nil {
        respondError(w, err)
        return
    }
    respondJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *httpServer) handlePauseTorrent(w http.ResponseWriter, r *http.Request) {
    infoHash := chi.URLParam(r, "infoHash")
    if err := s.manager.PauseTorrent(infoHash); err != nil {
        respondError(w, err)
        return
    }
    respondJSON(w, http.StatusOK, map[string]string{"status": "paused"})
}

func (s *httpServer) handleResumeTorrent(w http.ResponseWriter, r *http.Request) {
    infoHash := chi.URLParam(r, "infoHash")
    if err := s.manager.ResumeTorrent(infoHash); err != nil {
        respondError(w, err)
        return
    }
    respondJSON(w, http.StatusOK, map[string]string{"status": "resumed"})
}

func (s *httpServer) handleVerifyTorrent(w http.ResponseWriter, r *http.Request) {
    infoHash := chi.URLParam(r, "infoHash")
    if err := s.manager.VerifyTorrent(r.Context(), infoHash); err != nil {
        respondError(w, err)
        return
    }
    respondJSON(w, http.StatusAccepted, map[string]string{"status": "verification-started"})
}

func (s *httpServer) spaHandler(fs http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.URL.Path, "/api/") {
            http.NotFound(w, r)
            return
        }

        target := r.URL.Path
        if target == "" || target == "/" {
            http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
            return
        }

        clean := filepath.Clean(target)
        resolved := filepath.Join(s.staticDir, clean)
        absResolved, err := filepath.Abs(resolved)
        if err != nil {
            respondErrorWithStatus(w, err, http.StatusBadRequest)
            return
        }
        prefix := s.staticDir + string(os.PathSeparator)
        if absResolved != s.staticDir && !strings.HasPrefix(absResolved, prefix) {
            http.Error(w, "invalid path", http.StatusBadRequest)
            return
        }

        if info, err := os.Stat(absResolved); err == nil && !info.IsDir() {
            fs.ServeHTTP(w, r)
            return
        }

        http.ServeFile(w, r, filepath.Join(s.staticDir, "index.html"))
    })
}

func (s *httpServer) notFound(w http.ResponseWriter, r *http.Request) {
    respondErrorWithStatus(w, errors.New("resource not found"), http.StatusNotFound)
}

func decodeBase64Payload(value string) ([]byte, error) {
    parts := strings.SplitN(value, ",", 2)
    if len(parts) == 2 {
        value = parts[1]
    }
    return base64.StdEncoding.DecodeString(value)
}

func respondJSON(w http.ResponseWriter, status int, payload interface{}) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, err error) {
    respondErrorWithStatus(w, err, http.StatusBadRequest)
}

func respondErrorWithStatus(w http.ResponseWriter, err error, status int) {
    if errors.Is(err, torrent.ErrTorrentNotFound) {
        status = http.StatusNotFound
    }
    if errors.Is(err, torrent.ErrMetadataUnavailable) {
        status = http.StatusAccepted
    }
    if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
        status = http.StatusRequestTimeout
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(map[string]string{
        "error": err.Error(),
    })
}

type addTorrentRequest struct {
    MagnetURI      string `json:"magnetUri"`
    TorrentFile    string `json:"torrentFile"`
    TorrentURL     string `json:"torrentUrl"`
    SelectedFiles  []int  `json:"selectedFiles"`
    ApplySelection bool   `json:"applySelection"`
}

type selectionRequest struct {
    SelectedFiles  []int `json:"selectedFiles"`
    ApplySelection bool  `json:"applySelection"`
}

func (s *httpServer) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
    infoHash := chi.URLParam(r, "infoHash")
    fileIndexStr := chi.URLParam(r, "fileIndex")
    
    fileIndex, err := strconv.Atoi(fileIndexStr)
    if err != nil {
        respondError(w, errors.New("invalid file index"))
        return
    }

    filePath, fileInfo, err := s.manager.FilePath(r.Context(), infoHash, fileIndex)
    if err != nil {
        respondError(w, err)
        return
    }

    file, err := os.Open(filePath)
    if err != nil {
        respondError(w, errors.New("file not found or not downloaded: " + err.Error()))
        return
    }
    defer file.Close()

    // Get file stats to set Content-Length
    stat, err := file.Stat()
    if err != nil {
        respondError(w, errors.New("cannot stat file: " + err.Error()))
        return
    }

    fileName := fileInfo.Name()
    
    // Escape quotes in filename to prevent header injection
    safeFileName := strings.ReplaceAll(fileName, "\"", "\\\"")
    
    // Determine content type based on file extension
    contentType := "application/octet-stream"
    ext := strings.ToLower(filepath.Ext(fileName))
    switch ext {
    case ".mp4":
        contentType = "video/mp4"
    case ".webm":
        contentType = "video/webm"
    case ".mkv":
        contentType = "video/x-matroska"
    case ".avi":
        contentType = "video/x-msvideo"
    case ".mov":
        contentType = "video/quicktime"
    case ".m4v":
        contentType = "video/x-m4v"
    case ".mpg", ".mpeg":
        contentType = "video/mpeg"
    }
    
    // For video files, use inline disposition to allow browser playback
    // For others, force download
    disposition := "attachment"
    if strings.HasPrefix(contentType, "video/") {
        disposition = "inline"
    }
    
    w.Header().Set("Content-Disposition", disposition+"; filename=\""+safeFileName+"\"")
    w.Header().Set("Content-Type", contentType)
    w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
    w.Header().Set("Accept-Ranges", "bytes")
    w.Header().Set("Cache-Control", "public, max-age=3600")
    
    http.ServeContent(w, r, fileName, stat.ModTime(), file)
}

func (s *httpServer) handleDownloadZip(w http.ResponseWriter, r *http.Request) {
    infoHash := chi.URLParam(r, "infoHash")
    
    summary, err := s.manager.GetTorrent(r.Context(), infoHash)
    if err != nil {
        respondError(w, err)
        return
    }
    
    if summary.Files == nil || len(summary.Files) == 0 {
        respondError(w, errors.New("no files available for this torrent"))
        return
    }
    
    // Collect completed files
    completedFiles := []struct {
        path     string
        filePath string
    }{}
    
    for _, fileInfo := range summary.Files {
        // Only include files that are 100% complete
        if fileInfo.Progress < 100 {
            continue
        }
        
        filePath, stat, err := s.manager.FilePath(r.Context(), infoHash, fileInfo.Index)
        if err != nil {
            continue // Skip files we can't access
        }
        
        // Verify file exists
        if _, err := os.Stat(filePath); err != nil {
            continue
        }
        
        completedFiles = append(completedFiles, struct {
            path     string
            filePath string
        }{
            path:     stat.Name(),
            filePath: filePath,
        })
    }
    
    if len(completedFiles) == 0 {
        respondError(w, errors.New("no completed files available for download"))
        return
    }
    
    // Create temporary file for ZIP
    tempFile, err := os.CreateTemp("", "tordown-*.zip")
    if err != nil {
        respondError(w, errors.New("failed to create temporary file: " + err.Error()))
        return
    }
    tempPath := tempFile.Name()
    defer os.Remove(tempPath)
    defer tempFile.Close()
    
    // Create ZIP writer
    zipWriter := zip.NewWriter(tempFile)
    
    // Add each completed file to the zip
    for _, fileData := range completedFiles {
        file, err := os.Open(fileData.filePath)
        if err != nil {
            continue // Skip files that can't be opened
        }
        
        // Create entry in zip with the relative path from torrent
        zipEntry, err := zipWriter.Create(fileData.path)
        if err != nil {
            file.Close()
            continue
        }
        
        // Copy file contents to zip
        _, err = io.Copy(zipEntry, file)
        file.Close()
        
        if err != nil {
            // If copy fails, continue with next file
            continue
        }
    }
    
    // Close the zip writer to finalize the archive
    if err := zipWriter.Close(); err != nil {
        respondError(w, errors.New("failed to finalize zip: " + err.Error()))
        return
    }
    
    // Get the size of the completed ZIP file
    zipStat, err := tempFile.Stat()
    if err != nil {
        respondError(w, errors.New("failed to stat zip file: " + err.Error()))
        return
    }
    
    // Seek to beginning of temp file for reading
    if _, err := tempFile.Seek(0, 0); err != nil {
        respondError(w, errors.New("failed to seek zip file: " + err.Error()))
        return
    }
    
    // Set headers
    zipName := strings.ReplaceAll(summary.Name, "\"", "\\\"")
    if zipName == "" {
        zipName = infoHash
    }
    
    w.Header().Set("Content-Disposition", "attachment; filename=\""+zipName+".zip\"")
    w.Header().Set("Content-Type", "application/zip")
    w.Header().Set("Content-Length", strconv.FormatInt(zipStat.Size(), 10))
    w.Header().Set("Cache-Control", "no-cache")
    
    // Serve the ZIP file
    http.ServeContent(w, r, zipName+".zip", zipStat.ModTime(), tempFile)
}
