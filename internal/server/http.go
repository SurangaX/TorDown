package server

import (
    "archive/zip"
    "context"
    "encoding/base64"
    "encoding/json"
    "errors"
    "fmt"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "strconv"
    "strings"
    "sync"
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
        zipBuilds:   make(map[string]struct{}),
        zipStatus:   make(map[string]*zipBuildStatus),
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
    r.Use(timeoutExceptDownloads(60 * time.Second))

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

    zipBuildMu sync.Mutex
    zipBuilds  map[string]struct{}
    zipStatus  map[string]*zipBuildStatus
}

type zipBuildStatus struct {
    TotalBytes     int64
    ProcessedBytes int64
    StartedAt      time.Time
    UpdatedAt      time.Time
    Ready          bool
    Error          string
    DownloadURL    string
}

func (s *httpServer) mountAPI(r chi.Router) {
    r.Get("/health", s.handleHealth)
    r.Get("/stats", s.handleStats)
    r.Get("/system", s.handleSystemResources)
    r.Get("/cache/stats", s.handleCacheStats)
    r.Post("/data/cleanup", s.handleCleanupData)
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
        r.Delete("/files/{fileIndex}", s.handleDeleteFile)
        r.Get("/download-zip", s.handleDownloadZip)
    })
}

func (s *httpServer) handleCleanupData(w http.ResponseWriter, r *http.Request) {
    cleanupMode := strings.TrimSpace(r.URL.Query().Get("mode"))
    if cleanupMode == "" {
        cleanupMode = "all"
    }

    var orphanResult torrent.CleanupResult
    var zipRemoved []string
    var zipErr error

    orphanRemoved := int64(0)
    orphanCount := 0
    zipCount := 0

    // Clean up orphan data if requested
    if cleanupMode == "all" || cleanupMode == "orphan" {
        result, err := s.manager.CleanupOrphanData()
        if err != nil {
            respondErrorWithStatus(w, err, http.StatusInternalServerError)
            return
        }
        orphanResult = result
        orphanCount = len(result.Removed)
        orphanRemoved = int64(orphanCount)
    }

    // Clean up ZIP cache if requested
    if cleanupMode == "all" || cleanupMode == "zips" {
        var maxAge time.Duration
        if cleanupMode == "all" {
            maxAge = 0 // Force-clean all
        } else {
            maxAge = 30 * time.Minute // Only clean zips older than 30 min
        }
        zipRemoved, zipErr = cleanupTemporaryZipArchives(maxAge)
        if zipErr != nil && cleanupMode == "all" {
            respondErrorWithStatus(w, zipErr, http.StatusInternalServerError)
            return
        }
        zipCount = len(zipRemoved)
    }

    sizeFreed := int64(0)
    for _, path := range zipRemoved {
        if stat, err := os.Stat(path); err == nil {
            sizeFreed += stat.Size()
        }
    }

    respondJSON(w, http.StatusOK, map[string]interface{}{
        "mode":                cleanupMode,
        "orphanRemoved":       orphanResult.Removed,
        "orphanCount":         orphanCount,
        "tempZipRemoved":      zipRemoved,
        "tempZipCount":        zipCount,
        "totalRemovedCount":   orphanCount + zipCount,
        "sizeFreedBytes":      sizeFreed,
        "message":             buildCleanupMessage(orphanCount, zipCount, sizeFreed),
    })
}

func buildCleanupMessage(orphanCount, zipCount int, sizeFreed int64) string {
    var parts []string
    if orphanCount > 0 {
        parts = append(parts, fmt.Sprintf("%d orphan item(s)", orphanCount))
    }
    if zipCount > 0 {
        parts = append(parts, fmt.Sprintf("%d ZIP file(s)", zipCount))
    }
    if len(parts) == 0 {
        return "No items to clean up."
    }
    msg := "Cleaned up " + strings.Join(parts, " and ") + "."
    if sizeFreed > 0 {
        msg += fmt.Sprintf(" Freed: %s", formatBytes(sizeFreed))
    }
    return msg
}

func formatBytes(bytes int64) string {
    const unit = 1024
    if bytes < unit {
        return fmt.Sprintf("%d B", bytes)
    }
    div, exp := int64(unit), 0
    for n := bytes / unit; n >= unit; n /= unit {
        div *= unit
        exp++
    }
    return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
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

func (s *httpServer) handleCacheStats(w http.ResponseWriter, r *http.Request) {
    tmpDir := os.TempDir()
    zipCacheDir := filepath.Join(tmpDir, "tordown-zip-cache")
    
    zipCacheSize := int64(0)
    zipCacheCount := 0
    otherCacheSize := int64(0)
    otherCacheCount := 0
    
    // Scan ZIP cache directory
    if entries, err := os.ReadDir(zipCacheDir); err == nil {
        for _, entry := range entries {
            if !entry.IsDir() {
                if fileInfo, err := entry.Info(); err == nil {
                    zipCacheSize += fileInfo.Size()
                    zipCacheCount++
                }
            }
        }
    }
    
    // Scan root tmp directory for tordown-*.zip files
    if entries, err := os.ReadDir(tmpDir); err == nil {
        for _, entry := range entries {
            if !entry.IsDir() {
                name := entry.Name()
                if strings.HasPrefix(name, "tordown-") && strings.HasSuffix(name, ".zip") {
                    if fileInfo, err := entry.Info(); err == nil {
                        otherCacheSize += fileInfo.Size()
                        otherCacheCount++
                    }
                }
            }
        }
    }
    
    respondJSON(w, http.StatusOK, map[string]interface{}{
        "zipCache": map[string]interface{}{
            "size":  zipCacheSize,
            "count": zipCacheCount,
            "path":  zipCacheDir,
        },
        "otherCache": map[string]interface{}{
            "size":  otherCacheSize,
            "count": otherCacheCount,
            "path":  tmpDir,
        },
        "totalCacheSize": zipCacheSize + otherCacheSize,
        "totalCacheCount": zipCacheCount + otherCacheCount,
    })
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

func timeoutExceptDownloads(timeout time.Duration) func(http.Handler) http.Handler {
    timeoutMw := middleware.Timeout(timeout)

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            path := r.URL.Path
            if strings.HasSuffix(path, "/download-zip") || strings.Contains(path, "/files/") {
                next.ServeHTTP(w, r)
                return
            }

            timeoutMw(next).ServeHTTP(w, r)
        })
    }
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

func (s *httpServer) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
    infoHash := chi.URLParam(r, "infoHash")
    fileIndexStr := chi.URLParam(r, "fileIndex")

    fileIndex, err := strconv.Atoi(fileIndexStr)
    if err != nil {
        respondError(w, errors.New("invalid file index"))
        return
    }

    if err := s.manager.DeleteFile(r.Context(), infoHash, fileIndex); err != nil {
        respondError(w, err)
        return
    }

    respondJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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
    
    prepareOnly := r.URL.Query().Get("prepare") == "1"

    // Collect completed files
    completedFiles := make([]zipFileEntry, 0)
    
    for _, fileInfo := range summary.Files {
        // Only include files that are 100% complete
        if fileInfo.Progress < 100 {
            continue
        }
        
        filePath, _, err := s.manager.FilePath(r.Context(), infoHash, fileInfo.Index)
        if err != nil {
            continue // Skip files we can't access
        }
        
        // Verify file exists
        stat, err := os.Stat(filePath)
        if err != nil {
            continue
        }
        
        completedFiles = append(completedFiles, zipFileEntry{
            path:     fileInfo.Path,
            filePath: filePath,
            size:     stat.Size(),
        })
    }
    
    if len(completedFiles) == 0 {
        respondError(w, errors.New("no completed files available for download"))
        return
    }

    zipName := strings.ReplaceAll(summary.Name, "\"", "\\\"")
    if zipName == "" {
        zipName = infoHash
    }

    cacheDir := filepath.Join(os.TempDir(), "tordown-zip-cache")
    if err := os.MkdirAll(cacheDir, 0o755); err != nil {
        respondError(w, errors.New("failed to prepare zip cache: "+err.Error()))
        return
    }

    safeInfoHash := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(infoHash, "0x")))
    zipPath := filepath.Join(cacheDir, safeInfoHash+"-"+strconv.FormatInt(summary.BytesCompleted, 10)+".zip")
    downloadURL := "/api/torrents/" + safeInfoHash + "/download-zip"

    if _, err := os.Stat(zipPath); err == nil {
        if prepareOnly {
            respondJSON(w, http.StatusOK, map[string]interface{}{
                "status":      "ready",
                "downloadUrl": downloadURL,
                "progress":    100.0,
                "etaSeconds":  int64(0),
            })
            return
        }
        s.serveZipFromPath(w, r, zipName, zipPath)
        return
    }

    totalBytes := int64(0)
    for _, entry := range completedFiles {
        totalBytes += entry.size
    }

    s.ensureZipBuild(zipPath, completedFiles, totalBytes, downloadURL)

    if prepareOnly {
        status := s.getZipBuildStatus(zipPath)
        respondJSON(w, http.StatusAccepted, s.zipStatusResponse("building", status, downloadURL))
        return
    }

    respondErrorWithStatus(w, errors.New("zip is being prepared, try again shortly"), http.StatusAccepted)
}

type zipFileEntry struct {
    path     string
    filePath string
    size     int64
}

func (s *httpServer) ensureZipBuild(zipPath string, completedFiles []zipFileEntry, totalBytes int64, downloadURL string) {
    s.zipBuildMu.Lock()
    if _, building := s.zipBuilds[zipPath]; building {
        s.zipBuildMu.Unlock()
        return
    }
    s.zipBuilds[zipPath] = struct{}{}
    s.zipStatus[zipPath] = &zipBuildStatus{
        TotalBytes:     totalBytes,
        ProcessedBytes: 0,
        StartedAt:      time.Now(),
        UpdatedAt:      time.Now(),
        Ready:          false,
        DownloadURL:    downloadURL,
    }
    s.zipBuildMu.Unlock()

    go func() {
        var buildErr error
        defer func() {
            s.zipBuildMu.Lock()
            delete(s.zipBuilds, zipPath)
            if status, ok := s.zipStatus[zipPath]; ok {
                status.UpdatedAt = time.Now()
                status.Ready = buildErr == nil
                if buildErr != nil {
                    status.Error = buildErr.Error()
                }
            }
            s.zipBuildMu.Unlock()
        }()

        buildErr = buildZipArchive(zipPath, completedFiles, func(copied int64) {
            s.updateZipBuildProgress(zipPath, copied)
        })
    }()
}

func (s *httpServer) updateZipBuildProgress(zipPath string, copied int64) {
    s.zipBuildMu.Lock()
    defer s.zipBuildMu.Unlock()
    status, ok := s.zipStatus[zipPath]
    if !ok {
        return
    }
    status.ProcessedBytes += copied
    if status.ProcessedBytes > status.TotalBytes {
        status.ProcessedBytes = status.TotalBytes
    }
    status.UpdatedAt = time.Now()
}

func (s *httpServer) getZipBuildStatus(zipPath string) *zipBuildStatus {
    s.zipBuildMu.Lock()
    defer s.zipBuildMu.Unlock()
    status, ok := s.zipStatus[zipPath]
    if !ok {
        return nil
    }
    clone := *status
    return &clone
}

func (s *httpServer) zipStatusResponse(defaultStatus string, status *zipBuildStatus, fallbackURL string) map[string]interface{} {
    payload := map[string]interface{}{
        "status":         defaultStatus,
        "downloadUrl":    fallbackURL,
        "progress":       0.0,
        "etaSeconds":     int64(0),
        "processedBytes": int64(0),
        "totalBytes":     int64(0),
    }

    if status == nil {
        return payload
    }

    if status.DownloadURL != "" {
        payload["downloadUrl"] = status.DownloadURL
    }
    payload["processedBytes"] = status.ProcessedBytes
    payload["totalBytes"] = status.TotalBytes

    if status.TotalBytes > 0 {
        progress := float64(status.ProcessedBytes) / float64(status.TotalBytes) * 100
        if progress > 100 {
            progress = 100
        }
        payload["progress"] = progress
    }

    elapsed := time.Since(status.StartedAt).Seconds()
    if elapsed > 0 && status.ProcessedBytes > 0 && status.TotalBytes > status.ProcessedBytes {
        rate := float64(status.ProcessedBytes) / elapsed
        if rate > 0 {
            payload["etaSeconds"] = int64(float64(status.TotalBytes-status.ProcessedBytes) / rate)
        }
    }

    if status.Error != "" {
        payload["status"] = "error"
        payload["error"] = status.Error
        return payload
    }

    if status.Ready {
        payload["status"] = "ready"
        payload["progress"] = 100.0
        payload["etaSeconds"] = int64(0)
    }

    return payload
}

func (s *httpServer) serveZipFromPath(w http.ResponseWriter, r *http.Request, zipName, zipPath string) {
    zipFile, err := os.Open(zipPath)
    if err != nil {
        respondError(w, errors.New("failed to open zip: "+err.Error()))
        return
    }
    defer zipFile.Close()

    zipStat, err := zipFile.Stat()
    if err != nil {
        respondError(w, errors.New("failed to stat zip: "+err.Error()))
        return
    }

    w.Header().Set("Content-Disposition", "attachment; filename=\""+zipName+".zip\"")
    w.Header().Set("Content-Type", "application/zip")
    w.Header().Set("Content-Length", strconv.FormatInt(zipStat.Size(), 10))
    w.Header().Set("Accept-Ranges", "bytes")
    w.Header().Set("Cache-Control", "no-cache")

    http.ServeContent(w, r, zipName+".zip", zipStat.ModTime(), zipFile)
}

func buildZipArchive(zipPath string, completedFiles []zipFileEntry, onProgress func(int64)) error {
    tmpPath := zipPath + ".tmp"
    tmpFile, err := os.Create(tmpPath)
    if err != nil {
        return err
    }

    zipWriter := zip.NewWriter(tmpFile)
    for _, fileData := range completedFiles {
        src, err := os.Open(fileData.filePath)
        if err != nil {
            continue
        }

        entryPath := filepath.ToSlash(filepath.Clean(fileData.path))
        if entryPath == "" || entryPath == "." {
            src.Close()
            continue
        }

        h := &zip.FileHeader{Name: entryPath, Method: zip.Store}
        h.SetModTime(time.Now())
        dst, err := zipWriter.CreateHeader(h)
        if err != nil {
            src.Close()
            continue
        }

        copied, _ := io.Copy(dst, src)
        if onProgress != nil && copied > 0 {
            onProgress(copied)
        }
        src.Close()
    }

    if err := zipWriter.Close(); err != nil {
        tmpFile.Close()
        _ = os.Remove(tmpPath)
        return err
    }
    if err := tmpFile.Close(); err != nil {
        _ = os.Remove(tmpPath)
        return err
    }

    if err := os.Rename(tmpPath, zipPath); err != nil {
        _ = os.Remove(tmpPath)
        return err
    }

    return nil
}

func cleanupTemporaryZipArchives(olderThan time.Duration) ([]string, error) {
    tmpDir := os.TempDir()

    removed := make([]string, 0)
    cutoff := time.Now().Add(-olderThan)

    removedRoot, err := cleanupZipDir(tmpDir, cutoff, false)
    if err != nil {
        return nil, err
    }
    removed = append(removed, removedRoot...)

    cacheDir := filepath.Join(tmpDir, "tordown-zip-cache")
    removedCache, _ := cleanupZipDir(cacheDir, cutoff, true)
    removed = append(removed, removedCache...)

    return removed, nil
}

func cleanupZipDir(dir string, cutoff time.Time, removeAnyZip bool) ([]string, error) {
    entries, err := os.ReadDir(dir)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil
        }
        return nil, err
    }

    removed := make([]string, 0)

    for _, entry := range entries {
        name := entry.Name()
        if entry.IsDir() {
            continue
        }
        if removeAnyZip {
            if !strings.HasSuffix(name, ".zip") {
                continue
            }
        } else if !strings.HasPrefix(name, "tordown-") || !strings.HasSuffix(name, ".zip") {
            continue
        }

        full := filepath.Join(dir, name)
        info, err := entry.Info()
        if err != nil {
            continue
        }
        if info.ModTime().After(cutoff) {
            continue
        }

        if err := os.Remove(full); err != nil {
            continue
        }
        removed = append(removed, full)
    }

    return removed, nil
}
