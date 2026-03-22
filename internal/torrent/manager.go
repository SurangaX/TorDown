package torrent

import (
    "bytes"
    "context"
    "encoding/base64"
    "encoding/hex"
    "encoding/json"
    "errors"
    "io"
    "net/http"
    "os"
    "path/filepath"
    "sort"
    "strings"
    "sync"
    "time"

    atorrent "github.com/anacrolix/torrent"
    "github.com/anacrolix/torrent/metainfo"
)

const maxTorrentFileSize = 20 << 20 // 20 MiB safeguard when fetching remote torrent files

const persistedStateFileName = ".tordown-state.json"

var (
    // ErrTorrentNotFound indicates the requested torrent is not managed by the client.
    ErrTorrentNotFound    = errors.New("torrent not found")
    ErrMetadataUnavailable = errors.New("torrent metadata not yet available")
    errNoInput            = errors.New("provide magnetUri, torrentFile, or torrentUrl")
)

// Config defines the runtime options for the torrent manager.
type Config struct {
    DownloadDir string
    ListenPort  int
    Seed        bool
    NoUpload    bool
    HTTPClient  *http.Client
}

// AddOptions capture torrent preferences at add time.
type AddOptions struct {
    Files        []int // explicit file indices to download
    HasSelection bool  // true if Files should override defaults
}

type selectionOptions struct {
    indices      []int
    hasSelection bool
}

type rateSample struct {
    downloaded int64
    uploaded   int64
    timestamp  time.Time
}

type persistedTorrent struct {
    SourceType    string    `json:"sourceType"`
    Source        string    `json:"source"`
    SelectedFiles []int     `json:"selectedFiles,omitempty"`
    HasSelection  bool      `json:"hasSelection"`
    Paused        bool      `json:"paused"`
    AddedAt       time.Time `json:"addedAt"`
}

// Manager coordinates torrent lifecycle operations around an anacrolix client.
type Manager struct {
    client      *atorrent.Client
    downloadDir string
    baseCtx     context.Context
    httpClient  *http.Client

    mu      sync.RWMutex
    paused  map[string]struct{}
    created map[string]time.Time

    rateMu      sync.Mutex
    rateSamples map[string]rateSample

    selMu             sync.Mutex
    pendingSelections map[string]selectionOptions

    persistMu sync.Mutex
    statePath string
    state     map[string]persistedTorrent
}

// NewManager boots a torrent client with the provided configuration.
func NewManager(ctx context.Context, cfg Config) (*Manager, error) {
    if strings.TrimSpace(cfg.DownloadDir) == "" {
        return nil, errors.New("download directory is required")
    }

    absDir, err := filepath.Abs(cfg.DownloadDir)
    if err != nil {
        return nil, err
    }
    if err := os.MkdirAll(absDir, 0o755); err != nil {
        return nil, err
    }

    clientCfg := atorrent.NewDefaultClientConfig()
    clientCfg.DataDir = absDir
    clientCfg.Seed = cfg.Seed
    clientCfg.NoUpload = cfg.NoUpload
    clientCfg.DisableAggressiveUpload = true
    if cfg.ListenPort > 0 {
        clientCfg.ListenPort = cfg.ListenPort
    }

    client, err := atorrent.NewClient(clientCfg)
    if err != nil {
        return nil, err
    }

    if ctx == nil {
        ctx = context.Background()
    }

    httpClient := cfg.HTTPClient
    if httpClient == nil {
        httpClient = &http.Client{Timeout: 30 * time.Second}
    }

    mgr := &Manager{
        client:            client,
        downloadDir:       absDir,
        baseCtx:           ctx,
        httpClient:        httpClient,
        paused:            make(map[string]struct{}),
        created:           make(map[string]time.Time),
        rateSamples:       make(map[string]rateSample),
        pendingSelections: make(map[string]selectionOptions),
        statePath:         filepath.Join(absDir, persistedStateFileName),
        state:             make(map[string]persistedTorrent),
    }

    // Best-effort restore of prior torrents so completed files are reattached after restart.
    _ = mgr.loadStateAndRestore()

    return mgr, nil
}

// Close releases the underlying torrent client resources.
func (m *Manager) Close() {
    if m == nil || m.client == nil {
        return
    }
    _ = m.client.Close()
}

// AddMagnet registers a new torrent using a magnet URI.
func (m *Manager) AddMagnet(ctx context.Context, magnetURI string, opts AddOptions) (TorrentSummary, error) {
    if strings.TrimSpace(magnetURI) == "" {
        return TorrentSummary{}, errNoInput
    }

    t, err := m.client.AddMagnet(magnetURI)
    if err != nil {
        return TorrentSummary{}, err
    }

    summary := m.initializeTorrent(ctx, t, opts)
    m.setPersistedTorrent(summary.InfoHash, persistedTorrent{
        SourceType:    "magnet",
        Source:        magnetURI,
        SelectedFiles: append([]int(nil), opts.Files...),
        HasSelection:  opts.HasSelection,
        AddedAt:       time.Now(),
    })

    return summary, nil
}

// AddTorrentFile registers a torrent using the provided .torrent payload.
func (m *Manager) AddTorrentFile(ctx context.Context, data []byte, opts AddOptions) (TorrentSummary, error) {
    if len(data) == 0 {
        return TorrentSummary{}, errNoInput
    }

    mi, err := metainfo.Load(bytes.NewReader(data))
    if err != nil {
        return TorrentSummary{}, err
    }
    mi.SetDefaults()

    t, err := m.client.AddTorrent(mi)
    if err != nil {
        return TorrentSummary{}, err
    }

    summary := m.initializeTorrent(ctx, t, opts)
    m.setPersistedTorrent(summary.InfoHash, persistedTorrent{
        SourceType:    "torrent-file-b64",
        Source:        base64.StdEncoding.EncodeToString(data),
        SelectedFiles: append([]int(nil), opts.Files...),
        HasSelection:  opts.HasSelection,
        AddedAt:       time.Now(),
    })

    return summary, nil
}

// AddTorrentURL fetches a torrent file from a remote HTTP(S) source and registers it.
func (m *Manager) AddTorrentURL(ctx context.Context, rawURL string, opts AddOptions) (TorrentSummary, error) {
    if strings.TrimSpace(rawURL) == "" {
        return TorrentSummary{}, errNoInput
    }

    req, err := http.NewRequestWithContext(chooseContext(ctx, m.baseCtx), http.MethodGet, rawURL, nil)
    if err != nil {
        return TorrentSummary{}, err
    }

    resp, err := m.httpClient.Do(req)
    if err != nil {
        return TorrentSummary{}, err
    }
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        return TorrentSummary{}, errors.New("failed to download torrent file")
    }

    limited := io.LimitReader(resp.Body, maxTorrentFileSize)
    data, err := io.ReadAll(limited)
    if err != nil {
        return TorrentSummary{}, err
    }

    return m.AddTorrentFile(ctx, data, opts)
}

// initializeTorrent ensures the torrent begins downloading once metadata is ready.
func (m *Manager) initializeTorrent(ctx context.Context, t *atorrent.Torrent, opts AddOptions) TorrentSummary {
    infoHash := formatInfoHash(t.InfoHash())

    m.mu.Lock()
    m.created[infoHash] = time.Now()
    delete(m.paused, infoHash)
    m.mu.Unlock()

    selection := selectionOptions{
        indices:      append([]int(nil), opts.Files...),
        hasSelection: opts.HasSelection,
    }

    if opts.HasSelection {
        m.selMu.Lock()
        m.pendingSelections[infoHash] = selection
        m.selMu.Unlock()
    }

    go m.awaitMetadata(ctx, t, infoHash, selection)

    return m.buildSummary(t)
}

func (m *Manager) awaitMetadata(ctx context.Context, t *atorrent.Torrent, infoHash string, selection selectionOptions) {
    downloadCtx := chooseContext(ctx, m.baseCtx)

    if t.Info() != nil {
        m.applySelection(t, infoHash, selection)
        return
    }

    select {
    case <-t.GotInfo():
        m.applySelection(t, infoHash, selection)
    case <-downloadCtx.Done():
    }
}

func (m *Manager) applySelection(t *atorrent.Torrent, infoHash string, selection selectionOptions) {
    files := t.Files()
    if len(files) == 0 {
        return
    }

    if !selection.hasSelection {
        t.DownloadAll()
        m.selMu.Lock()
        delete(m.pendingSelections, infoHash)
        m.selMu.Unlock()
        return
    }

    for _, f := range files {
        f.SetPriority(atorrent.PiecePriorityNone)
    }

    if len(selection.indices) == 0 {
        t.DisallowDataDownload()
        m.selMu.Lock()
        delete(m.pendingSelections, infoHash)
        m.selMu.Unlock()
        return
    }

    t.AllowDataDownload()
    for _, idx := range selection.indices {
        if idx < 0 || idx >= len(files) {
            continue
        }
        f := files[idx]
        f.SetPriority(atorrent.PiecePriorityNormal)
        f.Download()
    }

    m.selMu.Lock()
    delete(m.pendingSelections, infoHash)
    m.selMu.Unlock()
}

// UpdateSelection adjusts the set of files marked for download for an existing torrent.
func (m *Manager) UpdateSelection(ctx context.Context, infoHash string, files []int, hasSelection bool) (TorrentSummary, error) {
    t, err := m.findTorrent(infoHash)
    if err != nil {
        return TorrentSummary{}, err
    }

    if _, err := m.waitForInfo(ctx, t); err != nil {
        return TorrentSummary{}, err
    }

    selection := selectionOptions{indices: append([]int(nil), files...), hasSelection: hasSelection}
    normalized := normalizeInfoHash(infoHash)
    m.applySelection(t, normalized, selection)
    m.updatePersistedSelection(normalized, files, hasSelection)

    return m.buildSummary(t), nil
}

// ListTorrents returns lightweight status for every tracked torrent.
func (m *Manager) ListTorrents() []TorrentSummary {
    torrents := m.client.Torrents()
    summaries := make([]TorrentSummary, 0, len(torrents))
    for _, t := range torrents {
        summaries = append(summaries, m.buildSummary(t))
    }
    sort.SliceStable(summaries, func(i, j int) bool {
        return summaries[i].Name < summaries[j].Name
    })
    return summaries
}

// GetTorrent returns a detailed view for a single torrent.
func (m *Manager) GetTorrent(ctx context.Context, infoHash string) (TorrentSummary, error) {
    t, err := m.findTorrent(infoHash)
    if err != nil {
        return TorrentSummary{}, err
    }

    summary := m.buildSummary(t)
    if err := m.populateFiles(ctx, t, &summary); err != nil {
        if errors.Is(err, ErrMetadataUnavailable) {
            return summary, ErrMetadataUnavailable
        }
        return TorrentSummary{}, err
    }
    return summary, nil
}

// RemoveTorrent drops the torrent from the client and optionally deletes data.
func (m *Manager) RemoveTorrent(infoHash string, deleteData bool) error {
    normalized := normalizeInfoHash(infoHash)
    t, err := m.findTorrent(normalized)
    if err != nil {
        return err
    }

    name := safeName(t.Name(), normalized)
    removeTargets := make([]string, 0, 4)
    if deleteData {
        removeTargets = append(removeTargets, filepath.Join(m.downloadDir, filepath.FromSlash(name)))

        // Capture per-file paths before dropping the torrent so single-file layouts are cleaned too.
        for _, f := range t.Files() {
            rel := filepath.FromSlash(f.DisplayPath())
            if rel == "." || rel == "" {
                continue
            }
            removeTargets = append(removeTargets,
                filepath.Join(m.downloadDir, filepath.FromSlash(name), rel),
                filepath.Join(m.downloadDir, rel),
            )
        }
    }

    t.Drop()

    m.mu.Lock()
    delete(m.paused, normalized)
    delete(m.created, normalized)
    m.mu.Unlock()

    m.rateMu.Lock()
    delete(m.rateSamples, normalized)
    m.rateMu.Unlock()

    m.selMu.Lock()
    delete(m.pendingSelections, normalized)
    m.selMu.Unlock()

    m.deletePersistedTorrent(normalized)

    if deleteData {
        if err := m.removeDataTargets(removeTargets); err != nil {
            return err
        }

        // Follow-up sweep catches partial leftovers created during interrupted downloads.
        if _, err := m.CleanupOrphanData(); err != nil {
            return err
        }
    }

    return nil
}

// CleanupOrphanData removes entries in downloadDir that are not referenced by active torrents.
func (m *Manager) CleanupOrphanData() (CleanupResult, error) {
    entries, err := os.ReadDir(m.downloadDir)
    if err != nil {
        return CleanupResult{}, err
    }

    protected := m.activeDataRoots()
    result := CleanupResult{Removed: make([]string, 0)}

    for _, entry := range entries {
        name := strings.TrimSpace(entry.Name())
        if name == "" {
            continue
        }
        if _, keep := protected[name]; keep {
            continue
        }

        target, ok := m.safeAbsWithinDownloadDir(filepath.Join(m.downloadDir, name))
        if !ok {
            continue
        }
        if err := os.RemoveAll(target); err != nil {
            return result, err
        }

        result.Removed = append(result.Removed, name)
    }

    sort.Strings(result.Removed)
    result.RemovedCount = len(result.Removed)
    return result, nil
}

func (m *Manager) activeDataRoots() map[string]struct{} {
    roots := make(map[string]struct{})
    torrents := m.client.Torrents()

    for _, t := range torrents {
        infoHash := formatInfoHash(t.InfoHash())
        nameRoot := topPathComponent(safeName(t.Name(), infoHash))
        if nameRoot != "" {
            roots[nameRoot] = struct{}{}
        }

        roots[infoHash] = struct{}{}

        for _, f := range t.Files() {
            fileRoot := topPathComponent(f.DisplayPath())
            if fileRoot != "" {
                roots[fileRoot] = struct{}{}
            }
        }
    }

    return roots
}

func topPathComponent(path string) string {
    if strings.TrimSpace(path) == "" {
        return ""
    }

    normalized := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
    normalized = strings.TrimPrefix(normalized, "./")
    if normalized == "." || normalized == "" {
        return ""
    }

    parts := strings.Split(normalized, "/")
    if len(parts) == 0 {
        return ""
    }
    return strings.TrimSpace(parts[0])
}

func (m *Manager) removeDataTargets(targets []string) error {
    seen := make(map[string]struct{}, len(targets))
    var firstErr error
    for _, target := range targets {
        abs, ok := m.safeAbsWithinDownloadDir(target)
        if !ok {
            continue
        }
        if _, exists := seen[abs]; exists {
            continue
        }
        seen[abs] = struct{}{}
        if err := os.RemoveAll(abs); err != nil && firstErr == nil {
            firstErr = err
        }
    }

    return firstErr
}

func (m *Manager) safeAbsWithinDownloadDir(target string) (string, bool) {
    if strings.TrimSpace(target) == "" {
        return "", false
    }

    abs, err := filepath.Abs(target)
    if err != nil {
        return "", false
    }

    cleanBase := filepath.Clean(m.downloadDir)
    cleanAbs := filepath.Clean(abs)
    if cleanAbs == cleanBase {
        return "", false
    }

    prefix := cleanBase + string(os.PathSeparator)
    if !strings.HasPrefix(cleanAbs, prefix) {
        return "", false
    }

    return cleanAbs, true
}

// PauseTorrent stops data transfer for the specified torrent.
func (m *Manager) PauseTorrent(infoHash string) error {
    normalized := normalizeInfoHash(infoHash)
    t, err := m.findTorrent(normalized)
    if err != nil {
        return err
    }

    t.DisallowDataDownload()
    t.DisallowDataUpload()

    m.mu.Lock()
    m.paused[normalized] = struct{}{}
    m.mu.Unlock()
    m.setPersistedPaused(normalized, true)
    return nil
}

// ResumeTorrent re-enables transfer for a paused torrent.
func (m *Manager) ResumeTorrent(infoHash string) error {
    normalized := normalizeInfoHash(infoHash)
    t, err := m.findTorrent(normalized)
    if err != nil {
        return err
    }

    t.AllowDataDownload()
    t.AllowDataUpload()

    m.selMu.Lock()
    selection, pending := m.pendingSelections[normalized]
    m.selMu.Unlock()
    if pending {
        m.applySelection(t, normalized, selection)
    } else {
        t.DownloadAll()
    }

    m.mu.Lock()
    delete(m.paused, normalized)
    m.mu.Unlock()
    m.setPersistedPaused(normalized, false)
    return nil
}

func (m *Manager) loadStateAndRestore() error {
    m.persistMu.Lock()
    if err := m.loadStateLocked(); err != nil {
        m.persistMu.Unlock()
        return err
    }

    snapshot := make(map[string]persistedTorrent, len(m.state))
    for k, v := range m.state {
        snapshot[k] = v
    }
    m.persistMu.Unlock()

    for key, entry := range snapshot {
        opts := AddOptions{HasSelection: entry.HasSelection, Files: append([]int(nil), entry.SelectedFiles...)}

        var (
            summary TorrentSummary
            err     error
        )

        switch entry.SourceType {
        case "magnet":
            summary, err = m.AddMagnet(m.baseCtx, entry.Source, opts)
        case "torrent-file-b64":
            raw, decErr := base64.StdEncoding.DecodeString(entry.Source)
            if decErr != nil {
                continue
            }
            summary, err = m.AddTorrentFile(m.baseCtx, raw, opts)
        default:
            continue
        }

        if err != nil {
            continue
        }

        normalized := normalizeInfoHash(summary.InfoHash)
        if normalized != key {
            m.persistMu.Lock()
            if persisted, ok := m.state[key]; ok {
                delete(m.state, key)
                m.state[normalized] = persisted
                _ = m.saveStateLocked()
            }
            m.persistMu.Unlock()
        }

        if entry.Paused {
            _ = m.PauseTorrent(normalized)
        }
    }

    return nil
}

func (m *Manager) loadStateLocked() error {
    data, err := os.ReadFile(m.statePath)
    if err != nil {
        if os.IsNotExist(err) {
            m.state = make(map[string]persistedTorrent)
            return nil
        }
        return err
    }

    decoded := make(map[string]persistedTorrent)
    if err := json.Unmarshal(data, &decoded); err != nil {
        return err
    }

    m.state = make(map[string]persistedTorrent, len(decoded))
    for hash, entry := range decoded {
        m.state[normalizeInfoHash(hash)] = entry
    }
    return nil
}

func (m *Manager) saveStateLocked() error {
    payload, err := json.MarshalIndent(m.state, "", "  ")
    if err != nil {
        return err
    }

    tmpPath := m.statePath + ".tmp"
    if err := os.WriteFile(tmpPath, payload, 0o644); err != nil {
        return err
    }
    return os.Rename(tmpPath, m.statePath)
}

func (m *Manager) setPersistedTorrent(infoHash string, entry persistedTorrent) {
    normalized := normalizeInfoHash(infoHash)
    m.persistMu.Lock()
    m.state[normalized] = entry
    _ = m.saveStateLocked()
    m.persistMu.Unlock()
}

func (m *Manager) deletePersistedTorrent(infoHash string) {
    normalized := normalizeInfoHash(infoHash)
    m.persistMu.Lock()
    delete(m.state, normalized)
    _ = m.saveStateLocked()
    m.persistMu.Unlock()
}

func (m *Manager) updatePersistedSelection(infoHash string, files []int, hasSelection bool) {
    normalized := normalizeInfoHash(infoHash)
    m.persistMu.Lock()
    entry, ok := m.state[normalized]
    if ok {
        entry.HasSelection = hasSelection
        entry.SelectedFiles = append([]int(nil), files...)
        m.state[normalized] = entry
        _ = m.saveStateLocked()
    }
    m.persistMu.Unlock()
}

func (m *Manager) setPersistedPaused(infoHash string, paused bool) {
    normalized := normalizeInfoHash(infoHash)
    m.persistMu.Lock()
    entry, ok := m.state[normalized]
    if ok {
        entry.Paused = paused
        m.state[normalized] = entry
        _ = m.saveStateLocked()
    }
    m.persistMu.Unlock()
}

// VerifyTorrent triggers a full recheck of downloaded data.
func (m *Manager) VerifyTorrent(ctx context.Context, infoHash string) error {
    normalized := normalizeInfoHash(infoHash)
    t, err := m.findTorrent(normalized)
    if err != nil {
        return err
    }

    go func() {
        _ = t.VerifyDataContext(chooseContext(ctx, m.baseCtx))
    }()
    return nil
}

// FilePath returns the absolute on-disk path for a torrent file index.
func (m *Manager) FilePath(ctx context.Context, infoHash string, index int) (string, os.FileInfo, error) {
    t, err := m.findTorrent(infoHash)
    if err != nil {
        return "", nil, err
    }

    if _, err := m.waitForInfo(ctx, t); err != nil {
        return "", nil, err
    }

    files := t.Files()
    if index < 0 || index >= len(files) {
        return "", nil, errors.New("file index out of range")
    }

    root := filepath.Join(m.downloadDir, safeName(t.Name(), infoHash))
    candidate := filepath.Join(root, filepath.FromSlash(files[index].DisplayPath()))
    absCandidate, err := filepath.Abs(candidate)
    if err != nil {
        return "", nil, err
    }

    prefix := m.downloadDir + string(os.PathSeparator)
    if absCandidate != m.downloadDir && !strings.HasPrefix(absCandidate, prefix) {
        return "", nil, errors.New("invalid file path")
    }

    info, err := os.Stat(absCandidate)
    if err != nil {
        return "", nil, err
    }

    if info.IsDir() {
        return "", nil, errors.New("requested path is a directory")
    }

    return absCandidate, info, nil
}

// Stats aggregates high-level metrics across all torrents.
func (m *Manager) Stats() ClientStats {
    stats := m.client.Stats()
    torrents := m.client.Torrents()
    return ClientStats{
        TotalTorrents: len(torrents),
        ActivePeers:   stats.TorrentGauges.ActivePeers,
        PendingPeers:  stats.TorrentGauges.PendingPeers,
        BytesDownloaded: stats.ConnStats.BytesReadUsefulData.Int64(),
        BytesUploaded:   stats.ConnStats.BytesWrittenData.Int64(),
    }
}

func (m *Manager) findTorrent(infoHash string) (*atorrent.Torrent, error) {
    ih, err := parseInfoHash(infoHash)
    if err != nil {
        return nil, err
    }
    if t, ok := m.client.Torrent(ih); ok {
        return t, nil
    }
    return nil, ErrTorrentNotFound
}

func (m *Manager) populateFiles(ctx context.Context, t *atorrent.Torrent, summary *TorrentSummary) error {
    info, err := m.waitForInfo(ctx, t)
    if err != nil {
        return err
    }
    if info == nil {
        return ErrMetadataUnavailable
    }

    files := t.Files()
    summary.Files = make([]TorrentFile, 0, len(files))
    for idx, f := range files {
        length := f.Length()
        completed := f.BytesCompleted()
        percent := 0.0
        if length > 0 {
            percent = float64(completed) / float64(length) * 100
        }
        priority := f.Priority()
        summary.Files = append(summary.Files, TorrentFile{
            Index:         idx,
            Path:          f.DisplayPath(),
            Length:        length,
            BytesCompleted: completed,
            Progress:      percent,
            Selected:      priority > atorrent.PiecePriorityNone,
        })
    }
    return nil
}

func (m *Manager) waitForInfo(ctx context.Context, t *atorrent.Torrent) (*metainfo.Info, error) {
    if info := t.Info(); info != nil {
        return info, nil
    }

    select {
    case <-t.GotInfo():
        return t.Info(), nil
    case <-chooseContext(ctx, m.baseCtx).Done():
        return nil, context.Canceled
    }
}

func (m *Manager) buildSummary(t *atorrent.Torrent) TorrentSummary {
    infoHash := formatInfoHash(t.InfoHash())
    bytesCompleted, total := selectedOrAllBytes(t)
    percent := 0.0
    if total > 0 {
        percent = float64(bytesCompleted) / float64(total) * 100
    }
    bytesMissing := total - bytesCompleted
    if bytesMissing < 0 {
        bytesMissing = 0
    }

    stats := t.Stats()

    key := normalizeInfoHash(infoHash)
    m.mu.RLock()
    _, paused := m.paused[key]
    createdAt := m.created[key]
    m.mu.RUnlock()

    downloadRate, uploadRate := m.computeRates(key, stats)
    var etaSeconds int64
    if downloadRate > 0 && bytesMissing > 0 {
        etaSeconds = int64(float64(bytesMissing) / downloadRate)
    }

    status := m.deriveStatus(t, paused, bytesCompleted, total, stats)

    return TorrentSummary{
        InfoHash:        infoHash,
        Name:            safeName(t.Name(), infoHash),
        Status:          status,
        Paused:          paused,
        Seeding:         t.Seeding(),
        BytesCompleted:  bytesCompleted,
        BytesMissing:    bytesMissing,
        TotalBytes:      total,
        Progress:        percent,
        BytesDownloaded: stats.ConnStats.BytesReadUsefulData.Int64(),
        BytesUploaded:   stats.ConnStats.BytesWrittenData.Int64(),
        DownloadRate:    downloadRate,
        UploadRate:      uploadRate,
        ETASeconds:      etaSeconds,
        ActivePeers:     stats.TorrentGauges.ActivePeers,
        AddedAt:         createdAt,
    }
}

func selectedOrAllBytes(t *atorrent.Torrent) (int64, int64) {
    files := t.Files()
    if len(files) == 0 {
        return t.BytesCompleted(), t.Length()
    }

    var selectedTotal int64
    var selectedCompleted int64
    var selectedCount int

    for _, f := range files {
        if f.Priority() <= atorrent.PiecePriorityNone {
            continue
        }
        selectedCount++
        selectedTotal += f.Length()
        selectedCompleted += f.BytesCompleted()
    }

    // If nothing is explicitly selected yet, fall back to full torrent totals.
    if selectedCount == 0 {
        return t.BytesCompleted(), t.Length()
    }

    return selectedCompleted, selectedTotal
}

func (m *Manager) computeRates(key string, stats atorrent.TorrentStats) (float64, float64) {
    m.rateMu.Lock()
    defer m.rateMu.Unlock()

    current := rateSample{
        downloaded: stats.ConnStats.BytesReadUsefulData.Int64(),
        uploaded:   stats.ConnStats.BytesWrittenData.Int64(),
        timestamp:  time.Now(),
    }

    prev, ok := m.rateSamples[key]
    m.rateSamples[key] = current
    if !ok || current.timestamp.Before(prev.timestamp) {
        return 0, 0
    }

    delta := current.timestamp.Sub(prev.timestamp).Seconds()
    if delta <= 0 {
        return 0, 0
    }

    dlRate := float64(current.downloaded-prev.downloaded) / delta
    ulRate := float64(current.uploaded-prev.uploaded) / delta
    if dlRate < 0 {
        dlRate = 0
    }
    if ulRate < 0 {
        ulRate = 0
    }

    return dlRate, ulRate
}

func (m *Manager) deriveStatus(t *atorrent.Torrent, paused bool, completed, total int64, stats atorrent.TorrentStats) string {
    switch {
    case paused:
        return "paused"
    case total == 0:
        return "fetching-metadata"
    case completed >= total && total > 0:
        if t.Seeding() {
            return "seeding"
        }
        return "completed"
    case stats.TorrentGauges.ActivePeers > 0:
        return "downloading"
    default:
        return "idle"
    }
}

func parseInfoHash(value string) (metainfo.Hash, error) {
    var h metainfo.Hash
    trimmed := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(value), "0x"))
    if len(trimmed) != 2*metainfo.HashSize {
        return metainfo.Hash{}, errors.New("invalid info hash length")
    }
    if err := h.FromHexString(trimmed); err != nil {
        return metainfo.Hash{}, err
    }
    return h, nil
}

func formatInfoHash(h metainfo.Hash) string {
    return strings.ToLower(hex.EncodeToString(h.Bytes()))
}

func safeName(name, fallback string) string {
    trimmed := strings.TrimSpace(name)
    if trimmed == "" {
        return fallback
    }
    return trimmed
}

func chooseContext(ctx, fallback context.Context) context.Context {
    if ctx != nil {
        return ctx
    }
    if fallback != nil {
        return fallback
    }
    return context.Background()
}

// TorrentSummary captures lightweight torrent telemetry for JSON APIs.
type TorrentSummary struct {
    InfoHash        string        `json:"infoHash"`
    Name            string        `json:"name"`
    Status          string        `json:"status"`
    Paused          bool          `json:"paused"`
    Seeding         bool          `json:"seeding"`
    BytesCompleted  int64         `json:"bytesCompleted"`
    BytesMissing    int64         `json:"bytesMissing"`
    TotalBytes      int64         `json:"totalBytes"`
    Progress        float64       `json:"progress"`
    BytesDownloaded int64         `json:"bytesDownloaded"`
    BytesUploaded   int64         `json:"bytesUploaded"`
    DownloadRate    float64       `json:"downloadRate"`
    UploadRate      float64       `json:"uploadRate"`
    ETASeconds      int64         `json:"etaSeconds"`
    ActivePeers     int           `json:"activePeers"`
    AddedAt         time.Time     `json:"addedAt"`
    Files           []TorrentFile `json:"files,omitempty"`
}

// TorrentFile describes progress for a single file in the torrent.
type TorrentFile struct {
    Index          int     `json:"index"`
    Path           string  `json:"path"`
    Length         int64   `json:"length"`
    BytesCompleted int64   `json:"bytesCompleted"`
    Progress       float64 `json:"progress"`
    Selected       bool    `json:"selected"`
}

// ClientStats aggregates totals from the torrent client.
type ClientStats struct {
    TotalTorrents  int   `json:"totalTorrents"`
    ActivePeers    int   `json:"activePeers"`
    PendingPeers   int   `json:"pendingPeers"`
    BytesDownloaded int64 `json:"bytesDownloaded"`
    BytesUploaded   int64 `json:"bytesUploaded"`
}

// CleanupResult reports what orphan data entries were removed from disk.
type CleanupResult struct {
    Removed      []string `json:"removed"`
    RemovedCount int      `json:"removedCount"`
}

func normalizeInfoHash(value string) string {
    return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "0x")))
}
