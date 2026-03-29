const API_BASE = "/api";
const POLL_INTERVAL_MS = 5000;

const state = {
  timer: null,
  detailsHash: null,
  messageTimeout: null,
};

// Initialize theme
function initializeTheme() {
  const storedTheme = localStorage.getItem('theme') || 'dark';
  document.documentElement.setAttribute('data-theme', storedTheme);
  updateThemeButton(storedTheme);
}

function toggleTheme() {
  const currentTheme = document.documentElement.getAttribute('data-theme');
  const newTheme = currentTheme === 'dark' ? 'light' : 'dark';
  document.documentElement.setAttribute('data-theme', newTheme);
  localStorage.setItem('theme', newTheme);
  updateThemeButton(newTheme);
}

function updateThemeButton(theme) {
  const btn = document.getElementById('theme-toggle-btn');
  if (btn) {
    btn.textContent = theme === 'dark' ? '☀️' : '🌙';
    btn.title = theme === 'dark' ? 'Switch to Light Mode' : 'Switch to Dark Mode';
  }
}

const elements = {};

// Video file extensions
const VIDEO_EXTENSIONS = ['.mp4', '.mkv', '.avi', '.mov', '.wmv', '.flv', '.webm', '.m4v', '.mpg', '.mpeg'];

document.addEventListener("DOMContentLoaded", () => {
  elements.form = document.getElementById("add-torrent-form");
  elements.refreshBtn = document.getElementById("refresh-btn");

    // Initialize theme
    initializeTheme();
    const themeToggleBtn = document.getElementById("theme-toggle-btn");
    if (themeToggleBtn) {
      themeToggleBtn.addEventListener("click", toggleTheme);
    }

  elements.clearDataBtn = document.getElementById("clear-data-btn");
  elements.tableBody = document.querySelector("#torrent-table tbody");
  elements.notificationBar = document.getElementById("notification-bar");
  elements.magnetInput = document.getElementById("magnet-uri");
  elements.torrentUrlInput = document.getElementById("torrent-url");
  elements.fileInput = document.getElementById("torrent-file");
  elements.uploadTrigger = document.getElementById("upload-trigger");
  elements.selectedFileName = document.getElementById("selected-file-name");
  elements.detailsModal = document.getElementById("details-modal");
  elements.detailsModalTitle = document.getElementById("details-modal-title");
  elements.detailsContent = document.getElementById("details-content");
  elements.closeDetails = document.getElementById("close-details");
  elements.sourceHint = document.getElementById("source-hint");
  elements.statTorrents = document.getElementById("stat-torrents");
  elements.statPeers = document.getElementById("stat-peers");
  elements.statDownloaded = document.getElementById("stat-downloaded");
  elements.statUploaded = document.getElementById("stat-uploaded");
  elements.mediaModal = document.getElementById("media-player-modal");
  elements.mediaPlayer = document.getElementById("media-player");
  elements.mediaSource = document.getElementById("media-source");
  elements.mediaTitle = document.getElementById("media-title");
  elements.closePlayer = document.getElementById("close-player");
  elements.selectionModal = document.getElementById("file-selection-modal");
  elements.selectionTitle = document.getElementById("selection-title");
  elements.selectionFilesList = document.getElementById("selection-files-list");
  elements.closeSelection = document.getElementById("close-selection");
  elements.selectAllFiles = document.getElementById("select-all-files");
  elements.selectNoneFiles = document.getElementById("select-none-files");
  elements.startDownloadSelected = document.getElementById("start-download-selected");
  elements.startDownloadAll = document.getElementById("start-download-all");
  
  // Cleanup modal elements
  elements.cleanupModal = document.getElementById("cleanup-modal");
  elements.closeCleanup = document.getElementById("close-cleanup");
  elements.cleanupCancelBtn = document.getElementById("cleanup-cancel-btn");
  elements.cleanupExecuteBtn = document.getElementById("cleanup-execute-btn");
  elements.cleanupCacheStats = document.getElementById("cleanup-cache-stats");
  elements.cleanupProgress = document.getElementById("cleanup-progress");
  elements.cleanupResult = document.getElementById("cleanup-result");
  elements.cleanupResultMessage = document.getElementById("cleanup-result-message");
  elements.cleanupResultDetails = document.getElementById("cleanup-result-details");

  elements.form.addEventListener("submit", onAddTorrentSubmit);
  elements.refreshBtn.addEventListener("click", () => refreshTorrents());
  if (elements.clearDataBtn) {
    elements.clearDataBtn.addEventListener("click", openCleanupModal);
  }
  elements.tableBody.addEventListener("click", onTableAction);
  elements.closeDetails.addEventListener("click", closeDetailsPanel);
  elements.detailsModal.addEventListener("click", (event) => {
    if (event.target === elements.detailsModal) {
      closeDetailsPanel();
    }
  });
  elements.detailsContent.addEventListener("click", onDetailsAction);
  elements.closePlayer.addEventListener("click", closeMediaPlayer);
  elements.closeSelection.addEventListener("click", closeFileSelectionModal);
  elements.selectAllFiles.addEventListener("click", () => toggleAllFileCheckboxes(true));
  elements.selectNoneFiles.addEventListener("click", () => toggleAllFileCheckboxes(false));
  elements.startDownloadSelected.addEventListener("click", confirmFileSelection);
  elements.startDownloadAll.addEventListener("click", downloadAllFiles);
  
  // Cleanup modal listeners
  elements.closeCleanup.addEventListener("click", closeCleanupModal);
  elements.cleanupModal.addEventListener("click", (event) => {
    if (event.target === elements.cleanupModal) {
      closeCleanupModal();
    }
  });
  elements.cleanupCancelBtn.addEventListener("click", closeCleanupModal);
  elements.cleanupExecuteBtn.addEventListener("click", executeCleanup);
  
  elements.magnetInput.addEventListener("input", syncSourceInputs);
  elements.torrentUrlInput.addEventListener("input", syncSourceInputs);
  elements.fileInput.addEventListener("change", syncSourceInputs);
  elements.form.addEventListener("reset", resetSourceInputs);

  syncSourceInputs();

  refreshTorrents();
  state.timer = setInterval(refreshTorrents, POLL_INTERVAL_MS);

  document.addEventListener("visibilitychange", onVisibilityChange);
});

function onVisibilityChange() {
  if (document.hidden && state.timer) {
    clearInterval(state.timer);
    state.timer = null;
  } else if (!document.hidden && !state.timer) {
    refreshTorrents();
    state.timer = setInterval(refreshTorrents, POLL_INTERVAL_MS);
  }
}

async function openCleanupModal() {
  // Reset modal state
  elements.cleanupProgress.hidden = true;
  elements.cleanupResult.hidden = true;
  elements.cleanupCancelBtn.hidden = false;
  elements.cleanupExecuteBtn.hidden = false;
  elements.cleanupExecuteBtn.disabled = false;
  
  // Fetch cache stats
  try {
    const stats = await apiRequest("GET", `${API_BASE}/cache/stats`);
    const zipSize = formatBytes(stats.zipCache?.size || 0);
    const otherSize = formatBytes(stats.otherCache?.size || 0);
    
    document.getElementById("cleanup-zip-cache-size").textContent = 
      `${zipSize} (${stats.zipCache?.count || 0} files)`;
    document.getElementById("cleanup-other-cache-size").textContent = 
      `${otherSize} (${stats.otherCache?.count || 0} files)`;
  } catch (error) {
    console.warn("Failed to fetch cache stats:", error);
    document.getElementById("cleanup-zip-cache-size").textContent = "Error loading";
    document.getElementById("cleanup-other-cache-size").textContent = "Error loading";
  }
  
  // Show modal
  elements.cleanupModal.hidden = false;
}

function closeCleanupModal() {
  elements.cleanupModal.hidden = true;
}

async function executeCleanup() {
  const mode = document.querySelector('input[name="cleanup-mode"]:checked')?.value || "all";
  
  // Show progress
  elements.cleanupProgress.hidden = false;
  elements.cleanupResult.hidden = true;
  elements.cleanupCancelBtn.hidden = true;
  elements.cleanupExecuteBtn.disabled = true;
  
  try {
    const result = await apiRequest("POST", `${API_BASE}/data/cleanup?mode=${mode}`, {});
    
    // Hide progress
    elements.cleanupProgress.hidden = true;
    
    // Show result
    elements.cleanupResult.hidden = false;
    elements.cleanupResultMessage.textContent = result.message || "Cleanup completed";
    
    let detailsHTML = `
      <div class="result-stat">
        <span class="result-label">Orphan items removed:</span>
        <span class="result-value">${result.orphanCount || 0}</span>
      </div>
      <div class="result-stat">
        <span class="result-label">ZIP files removed:</span>
        <span class="result-value">${result.tempZipCount || 0}</span>
      </div>
      <div class="result-stat">
        <span class="result-label">Space freed:</span>
        <span class="result-value">${formatBytes(result.sizeFreedBytes || 0)}</span>
      </div>
    `;
    
    if (result.orphanRemoved && result.orphanRemoved.length > 0) {
      detailsHTML += `<div class="result-section"><strong>Removed items:</strong><ul>${
        result.orphanRemoved.map(item => `<li>${escapeHtml(item)}</li>`).join('')
      }</ul></div>`;
    }
    
    elements.cleanupResultDetails.innerHTML = detailsHTML;
    
    showMessage(result.message || "Cleanup completed successfully", false);
    await refreshTorrents();
    
  } catch (error) {
    // Hide progress
    elements.cleanupProgress.hidden = true;
    
    // Show error
    elements.cleanupResult.hidden = false;
    elements.cleanupResultMessage.textContent = "Cleanup failed";
    elements.cleanupResultDetails.innerHTML = `<div class="result-error">${escapeHtml(error.message)}</div>`;
    
    showMessage(error.message || "Cleanup failed", true);
  } finally {
    elements.cleanupCancelBtn.hidden = false;
    elements.cleanupExecuteBtn.disabled = false;
  }
}

async function onClearDataClick() {
  const warning = "Clear orphan data from server disk? Active torrent files will be kept.";
  if (!confirm(warning)) {
    return;
  }

  try {
    const result = await apiRequest("POST", `${API_BASE}/data/cleanup`, {});
    const removedCount = result && Number.isFinite(result.removedCount) ? result.removedCount : 0;
    const tempZipRemovedCount = result && Number.isFinite(result.tempZipRemovedCount) ? result.tempZipRemovedCount : 0;
    const totalRemoved = result && Number.isFinite(result.totalRemovedCount)
      ? result.totalRemovedCount
      : (removedCount + tempZipRemovedCount);
    if (totalRemoved > 0) {
      showMessage(`Cleared ${removedCount} orphan item(s) and ${tempZipRemovedCount} temp ZIP file(s).`, false);
    } else {
      showMessage("No orphan data found.", false);
    }
    await refreshTorrents();
  } catch (error) {
    showMessage(error.message, true);
  }
}

async function onAddTorrentSubmit(event) {
  event.preventDefault();

  const magnetUri = elements.magnetInput.value.trim();
  const torrentUrl = elements.torrentUrlInput.value.trim();
  const file = elements.fileInput.files[0];

  const providedSources = [magnetUri, torrentUrl, file ? "file" : ""].filter(Boolean).length;

  if (providedSources === 0) {
    showMessage("Provide exactly one source: magnet link, torrent URL, or upload a .torrent file.", true);
    return;
  }
  if (providedSources > 1) {
    showMessage("Provide exactly one source: magnet link, torrent URL, or upload a .torrent file.", true);
    return;
  }

  const payload = {};
  if (magnetUri) {
    payload.magnetUri = magnetUri;
  } else if (torrentUrl) {
    payload.torrentUrl = torrentUrl;
  } else if (file) {
    const arrayBuffer = await file.arrayBuffer();
    payload.torrentFile = base64FromArrayBuffer(arrayBuffer);
  }

  try {
    const response = await apiRequest("POST", `${API_BASE}/torrents`, payload);
    elements.form.reset();
    resetSourceInputs();
    showMessage("Torrent added. Fetching metadata...", false);
    await refreshTorrents();
    
    // Wait for metadata and show file selection
    if (response && response.infoHash) {
      await waitForMetadataAndShowSelection(response.infoHash);
    }
  } catch (error) {
    showMessage(error.message, true);
  }
}

async function waitForMetadataAndShowSelection(infoHash) {
  const maxAttempts = 30; // 30 seconds max wait
  let attempts = 0;
  
  while (attempts < maxAttempts) {
    await new Promise(resolve => setTimeout(resolve, 1000));
    attempts++;
    
    try {
      const result = await apiRequest("GET", `${API_BASE}/torrents/${infoHash}`, null, { returnMeta: true });
      const { payload, status } = result;
      
      // If metadata is ready (status 200)
      if (status === 200 && payload.files && payload.files.length > 0) {
        showFileSelectionModal(payload);
        return;
      }
    } catch (error) {
      console.warn("Error waiting for metadata:", error);
      break;
    }
  }
  
  showMessage("Metadata fetch timeout. You can select files later from torrent details.", false);
}

async function refreshTorrents(options = {}) {
  try {
    const [torrentsResult, statsResult, systemResult] = await Promise.allSettled([
      apiRequest("GET", `${API_BASE}/torrents`),
      apiRequest("GET", `${API_BASE}/stats`),
      apiRequest("GET", `${API_BASE}/system`),
    ]);

    if (torrentsResult.status !== "fulfilled") {
      throw torrentsResult.reason;
    }

    renderTorrents(torrentsResult.value);

    if (statsResult.status === "fulfilled") {
      renderStats(statsResult.value);
    } else {
      console.warn("Failed to refresh stats", statsResult.reason);
    }

    if (systemResult.status === "fulfilled") {
      renderSystemResources(systemResult.value);
    } else {
      console.warn("Failed to refresh system resources", systemResult.reason);
    }

    if (!options.skipDetailUpdate && state.detailsHash) {
      await loadTorrentDetails(state.detailsHash, { silent: true, fromRefresh: true });
    }
  } catch (error) {
    showMessage(error.message, true);
  }
}

function renderTorrents(torrents) {
  const tbody = elements.tableBody;
  if (!Array.isArray(torrents) || torrents.length === 0) {
    tbody.innerHTML = `<tr><td colspan="8" class="empty">No torrents yet.</td></tr>`;
    return;
  }

  tbody.innerHTML = torrents
    .map((torrent) => {
      const safeName = escapeHtml(torrent.name || torrent.infoHash);
      const statusClass = torrent.status ? `status-pill ${torrent.status}` : "status-pill";
      const progress = clampNumber(torrent.progress || 0, 0, 100);
      const progressLabel = `${progress.toFixed(1)}%`;
      const downloadRate = formatRate(torrent.downloadRate);
      const uploadRate = formatRate(torrent.uploadRate);
      const eta = formatEta(torrent.etaSeconds);
      const downloaded = formatBytes(torrent.bytesCompleted || 0);
      const totalBytes = formatBytes(torrent.totalBytes || 0);
      const uploaded = formatBytes(torrent.bytesUploaded || 0);
      const infoHash = torrent.infoHash;
    const paused = Boolean(torrent.paused);
    const resumeLabel = paused ? "Resume" : "Pause";
    const resumeAction = paused ? "resume" : "pause";
    const isActive = state.detailsHash === infoHash && !elements.detailsModal.hidden;
    const detailsLabel = isActive ? "Hide" : "Details";
    const rowClass = isActive ? ' class="active-row"' : "";

      return `
        <tr${rowClass}>
          <td class="name">${safeName}</td>
          <td><span class="${statusClass}">${escapeHtml(torrent.status || "unknown")}</span></td>
          <td>
            <div class="progress-bar"><span style="width: ${progress}%"></span></div>
            <small>${progressLabel}</small>
          </td>
          <td>
            <div class="stat-block">
              <span>↓ ${downloadRate}</span>
              <span>↑ ${uploadRate}</span>
            </div>
          </td>
          <td>${eta}</td>
          <td>${torrent.activePeers ?? 0}</td>
          <td>
            <div class="stat-block">
              <span>↓ ${downloaded} / ${totalBytes}</span>
              <span>↑ ${uploaded}</span>
            </div>
          </td>
          <td>
            <div class="actions">
              <button type="button" data-action="${resumeAction}" data-hash="${infoHash}">${resumeLabel}</button>
              <button type="button" data-action="details" data-hash="${infoHash}">${detailsLabel}</button>
              <button type="button" data-action="download-zip" data-hash="${infoHash}" class="action-download-zip" title="Download as ZIP">📦 ZIP</button>
              <button type="button" data-action="remove" data-hash="${infoHash}">Remove</button>
            </div>
          </td>
        </tr>
      `;
    })
    .join("");
}

async function onTableAction(event) {
  const button = event.target.closest("button[data-action]");
  if (!button) {
    return;
  }

  const { action, hash } = button.dataset;

  try {
    switch (action) {
      case "pause":
        await apiRequest("POST", `${API_BASE}/torrents/${hash}/pause`);
        showMessage("Torrent paused.", false);
        break;
      case "resume":
        await apiRequest("POST", `${API_BASE}/torrents/${hash}/resume`);
        showMessage("Torrent resumed.", false);
        break;
      case "remove":
        if (!confirm("Permanently remove this torrent and its downloaded data from the server?")) {
          return;
        }
        await apiRequest("DELETE", `${API_BASE}/torrents/${hash}?deleteData=true`);
        showMessage("Torrent and data removed.", false);
        break;
      case "details":
        if (state.detailsHash === hash && !elements.detailsModal.hidden) {
          closeDetailsPanel();
          await refreshTorrents({ skipDetailUpdate: true });
          return;
        }
        await loadTorrentDetails(hash);
        return;
      case "download-zip":
        await startZipDownload(hash);
        return;
      default:
        return;
    }

    await refreshTorrents();
  } catch (error) {
    showMessage(error.message, true);
  }
}

async function loadTorrentDetails(infoHash, options = {}) {
  try {
    if (!options.silent) {
      showDetailsLoading();
    }
    const result = await apiRequest("GET", `${API_BASE}/torrents/${infoHash}`, null, { returnMeta: true });
    const { payload, status } = result;

    state.detailsHash = infoHash;
    const metadataPending = status === 202;
    renderTorrentDetails(payload, metadataPending);
    elements.detailsModal.hidden = false;
    setRowActive(infoHash, true);

    if (!options.silent && metadataPending) {
      showMessage("Metadata is still being fetched. File list will appear once ready.", false);
    }
  } catch (error) {
    if (!options.silent) {
      showMessage(error.message, true);
    }
    if (!options.keepPanel) {
      closeDetailsPanel();
      setRowActive(infoHash, false);
    }
  }
}

function renderTorrentDetails(summary, metadataPending) {
  if (!summary || !summary.infoHash) {
    elements.detailsContent.innerHTML = `<p class="info-banner">Torrent details unavailable.</p>`;
    return;
  }

  const safeName = escapeHtml(summary.name || summary.infoHash);
  
  // Set modal title
  elements.detailsModalTitle.textContent = safeName || 'Torrent Details';
  
  const statusClass = summary.status ? `status-pill ${summary.status}` : "status-pill";
  const downloadRate = formatRate(summary.downloadRate);
  const uploadRate = formatRate(summary.uploadRate);
  const eta = formatEta(summary.etaSeconds);
  const progress = clampNumber(summary.progress || 0, 0, 100).toFixed(1);
  const downloaded = formatBytes(summary.bytesCompleted || 0);
  const totalBytes = formatBytes(summary.totalBytes || 0);
  const uploaded = formatBytes(summary.bytesUploaded || 0);
  const resumeLabel = summary.paused ? "Resume" : "Pause";
  const resumeAction = summary.paused ? "resume" : "pause";

  let filesMarkup = "";
  if (metadataPending) {
    filesMarkup = `<p class="info-banner">Metadata is still downloading. File selection will be enabled after discovery completes.</p>`;
  } else if (!summary.files || summary.files.length === 0) {
    filesMarkup = `<p class="info-banner">No files reported for this torrent yet.</p>`;
  } else {
    const rows = summary.files
      .map((file) => {
        const checked = file.selected ? "checked" : "";
        const progressLabel = clampNumber(file.progress || 0, 0, 100).toFixed(1);
        const isComplete = file.progress >= 100;
        const isVideo = isVideoFile(file.path);
        
        let actionButtons = '';
        if (isComplete) {
          if (isVideo) {
            actionButtons = `
              <button class="play-file-btn" data-file-index="${file.index}" data-file-path="${escapeHtml(file.path)}" title="Play video">▶️ Play</button>
              <a href="${API_BASE}/torrents/${escapeHtml(summary.infoHash)}/files/${file.index}" class="download-file-btn" download title="Download to PC">⬇</a>
            `;
          } else {
            actionButtons = `<a href="${API_BASE}/torrents/${escapeHtml(summary.infoHash)}/files/${file.index}" class="download-file-btn" download title="Download to PC">⬇ Download</a>`;
          }
        } else {
          actionButtons = `<button class="delete-file-btn" data-file-index="${file.index}" title="Delete incomplete file from disk">🗑 Delete</button>`;
        }
        
        return `
          <tr>
            <td><input type="checkbox" data-index="${file.index}" ${checked}></td>
            <td>${file.index}</td>
            <td class="file-path">${escapeHtml(file.path)}</td>
            <td>${formatBytes(file.length)}</td>
            <td>${formatBytes(file.bytesCompleted || 0)}</td>
            <td>${progressLabel}%</td>
            <td class="file-actions">${actionButtons}</td>
          </tr>
        `;
      })
      .join("");

    filesMarkup = `
      <div class="details-actions">
        <button type="button" data-details-action="select-all">Select All</button>
        <button type="button" data-details-action="select-none">Select None</button>
        <button type="button" data-details-action="download-all">Download All</button>
        <button type="button" data-details-action="apply-selection">Apply Selection</button>
      </div>
      <div class="table-wrapper">
        <table class="files-table">
          <thead>
            <tr>
              <th></th>
              <th>#</th>
              <th>Path</th>
              <th>Size</th>
              <th>Downloaded</th>
              <th>Progress</th>
              <th>Download</th>
            </tr>
          </thead>
          <tbody>
            ${rows}
          </tbody>
        </table>
      </div>
    `;
  }

  elements.detailsContent.innerHTML = `
    <div class="details-quick-actions">
      <button type="button" data-details-action="${resumeAction}" class="btn-action">${resumeLabel}</button>
      <button type="button" data-details-action="refresh" class="btn-action">Refresh</button>
      <button type="button" data-details-action="download-zip" class="download-zip-btn">📦 Download ZIP</button>
      <button type="button" data-details-action="remove-data" class="btn-danger">Delete</button>
    </div>
    
    <section class="details-meta">
      <div class="meta-item"><span class="label">Status</span><span class="value"><span class="${statusClass}">${escapeHtml(summary.status || "unknown")}</span></span></div>
      <div class="meta-item"><span class="label">Progress</span><span class="value">${progress}%</span></div>
      <div class="meta-item"><span class="label">Download</span><span class="value">${downloadRate}</span></div>
      <div class="meta-item"><span class="label">Upload</span><span class="value">${uploadRate}</span></div>
      <div class="meta-item"><span class="label">ETA</span><span class="value">${eta}</span></div>
      <div class="meta-item"><span class="label">Peers</span><span class="value">${summary.activePeers ?? 0}</span></div>
      <div class="meta-item"><span class="label">Downloaded</span><span class="value">${downloaded} / ${totalBytes}</span></div>
      <div class="meta-item"><span class="label">Uploaded</span><span class="value">${uploaded}</span></div>
    </section>
    
    <div class="details-info-hash">
      <span class="label">Info Hash:</span>
      <code>${summary.infoHash}</code>
    </div>
    
    ${filesMarkup}
  `;

  const applyButton = elements.detailsContent.querySelector('[data-details-action="apply-selection"]');
  const selectButtons = elements.detailsContent.querySelectorAll('[data-details-action="select-all"], [data-details-action="select-none"], [data-details-action="download-all"]');
  if (applyButton) {
    applyButton.disabled = metadataPending;
  }
  selectButtons.forEach((btn) => {
    btn.disabled = metadataPending;
  });
}

async function onDetailsAction(event) {
  const button = event.target.closest("[data-details-action]");
  if (!button || !state.detailsHash) {
    return;
  }

  const action = button.dataset.detailsAction;

  switch (action) {
    case "refresh":
      await loadTorrentDetails(state.detailsHash);
      break;
    case "pause":
      await apiRequest("POST", `${API_BASE}/torrents/${state.detailsHash}/pause`);
      showMessage("Torrent paused.", false);
      await refreshTorrents();
      await loadTorrentDetails(state.detailsHash, { silent: true });
      break;
    case "resume":
      await apiRequest("POST", `${API_BASE}/torrents/${state.detailsHash}/resume`);
      showMessage("Torrent resumed.", false);
      await refreshTorrents();
      await loadTorrentDetails(state.detailsHash, { silent: true });
      break;
    case "select-all":
      setAllFileCheckboxes(true);
      break;
    case "select-none":
      setAllFileCheckboxes(false);
      break;
    case "download-all":
      await submitSelection(state.detailsHash, false, []);
      break;
    case "download-zip":
      await startZipDownload(state.detailsHash);
      break;
    case "apply-selection":
      await submitSelection(state.detailsHash, true, collectSelectedFileIndices());
      break;
    case "remove":
      if (confirm("Remove torrent from the session?")) {
        try {
          await apiRequest("DELETE", `${API_BASE}/torrents/${state.detailsHash}`);
          showMessage("Torrent removed.", false);
          closeDetailsPanel();
          await refreshTorrents();
        } catch (error) {
          showMessage(error.message, true);
        }
      }
      break;
    case "remove-data":
      if (confirm("Remove torrent and delete downloaded data?")) {
        try {
          await apiRequest("DELETE", `${API_BASE}/torrents/${state.detailsHash}?deleteData=true`);
          showMessage("Torrent and data removed.", false);
          closeDetailsPanel();
          await refreshTorrents();
        } catch (error) {
          showMessage(error.message, true);
        }
      }
      break;
    default:
      return;
  }
}

async function onFileDownload(event) {
  const button = event.target.closest(".download-file-btn");
  if (!button || !state.detailsHash) {
    return;
  }

  const fileIndex = button.dataset.fileIndex;
  const downloadUrl = `${API_BASE}/torrents/${state.detailsHash}/files/${fileIndex}`;
  
  try {
    // Create a temporary anchor element to trigger download
    const a = document.createElement('a');
    a.href = downloadUrl;
    a.download = ''; // Let server specify filename via Content-Disposition
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    
    showMessage("Download started.", false);
  } catch (error) {
    showMessage("Download failed: " + error.message, true);
  }
}

function collectSelectedFileIndices() {
  const checkboxes = elements.detailsContent.querySelectorAll("input[type='checkbox'][data-index]");
  const indices = [];
  checkboxes.forEach((box) => {
    if (box.checked) {
      indices.push(Number.parseInt(box.dataset.index, 10));
    }
  });
  return indices;
}

function setAllFileCheckboxes(checked) {
  const checkboxes = elements.detailsContent.querySelectorAll("input[type='checkbox'][data-index]");
  checkboxes.forEach((box) => {
    box.checked = checked;
  });
}

async function submitSelection(infoHash, applySelection, indices) {
  try {
    const payload = { applySelection };
    if (applySelection) {
      payload.selectedFiles = indices;
    }
    await apiRequest("POST", `${API_BASE}/torrents/${infoHash}/selection`, payload);
    showMessage(applySelection ? "File selection updated." : "Restored download of all files.", false);
    await refreshTorrents({ skipDetailUpdate: true });
    await loadTorrentDetails(infoHash, { silent: true, keepPanel: true });
  } catch (error) {
    showMessage(error.message, true);
  }
}

function closeDetailsPanel() {
  const previousHash = state.detailsHash;

  elements.detailsModal.hidden = true;
  elements.detailsContent.innerHTML = "";
  state.detailsHash = null;

  if (previousHash) {
    setRowActive(previousHash, false);
  }
}

async function apiRequest(method, url, body, options = {}) {
  const requestOptions = {
    method,
    headers: {
      Accept: "application/json",
    },
  };

  if (body && method !== "GET" && method !== "DELETE") {
    requestOptions.headers["Content-Type"] = "application/json";
    requestOptions.body = JSON.stringify(body);
  }

  const response = await fetch(url, requestOptions);
  const contentType = response.headers.get("Content-Type") || "";

  let payload = null;
  if (contentType.includes("application/json")) {
    payload = await response.json();
  } else if (method !== "GET" && method !== "HEAD") {
    payload = await response.text();
  }

  if (!response.ok) {
    const message = payload && payload.error ? payload.error : response.statusText;
    throw new Error(message || "Request failed");
  }

  if (options.returnMeta) {
    return { payload, status: response.status };
  }

  return payload;
}

async function startZipDownload(infoHash) {
  const prepareUrl = `${API_BASE}/torrents/${infoHash}/download-zip?prepare=1`;
  const downloadUrl = `${API_BASE}/torrents/${infoHash}/download-zip`;
  const maxAttempts = 180;

  renderZipPrepareProgress({ progress: 0, etaSeconds: 0, processedBytes: 0, totalBytes: 0 });

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    const result = await apiRequest("GET", prepareUrl, null, { returnMeta: true });
    if (result.payload && result.payload.status === "error") {
      showMessage(result.payload.error || "ZIP preparation failed.", true);
      return;
    }

    if (result.payload && (result.payload.status === "building" || result.status === 202)) {
      renderZipPrepareProgress(result.payload);
    }

    if (result.status === 200 && result.payload && result.payload.status === "ready") {
      const a = document.createElement("a");
      a.href = (result.payload.downloadUrl || downloadUrl);
      a.download = "";
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      showMessage("ZIP download started. You can pause/resume in your browser download manager.", false);
      return;
    }

    await new Promise((resolve) => setTimeout(resolve, 1000));
  }

  showMessage("ZIP preparation is taking longer than expected. Try again in a minute.", true);
}

function showMessage(message, isError) {
  if (state.messageTimeout) {
    clearTimeout(state.messageTimeout);
    state.messageTimeout = null;
  }

  if (!message) {
    elements.notificationBar.textContent = "";
    elements.notificationBar.className = "notification-bar";
    return;
  }

  elements.notificationBar.textContent = message;
  elements.notificationBar.className = `notification-bar ${isError ? "notification-error" : "notification-success"} notification-show`;

  if (!isError) {
    state.messageTimeout = setTimeout(() => {
      clearMessage();
    }, 5000);
  }
}

function renderZipPrepareProgress(payload) {
  const progress = clampNumber(payload?.progress || 0, 0, 100);
  const processedBytes = Number.isFinite(payload?.processedBytes) ? payload.processedBytes : 0;
  const totalBytes = Number.isFinite(payload?.totalBytes) ? payload.totalBytes : 0;
  const etaLabel = formatEta(payload?.etaSeconds || 0);

  const detail = totalBytes > 0
    ? `${formatBytes(processedBytes)} / ${formatBytes(totalBytes)}`
    : `${formatBytes(processedBytes)} processed`;

  elements.notificationBar.innerHTML = `
    <div class="zip-progress-wrap">
      <div class="zip-progress-title">Preparing ZIP for resumable download...</div>
      <div class="zip-progress-bar"><span style="width:${progress.toFixed(1)}%"></span></div>
      <div class="zip-progress-meta">
        <span>${progress.toFixed(1)}% (${detail})</span>
        <span>ETA ${etaLabel}</span>
      </div>
    </div>
  `;
  elements.notificationBar.className = "notification-bar notification-success notification-show";
}

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return "0 B";
  }
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  const exponent = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  const value = bytes / Math.pow(1024, exponent);
  const decimals = exponent === 0 ? 0 : value < 10 ? 2 : 1;
  return `${value.toFixed(decimals)} ${units[exponent]}`;
}

function formatRate(rate) {
  if (!Number.isFinite(rate) || rate <= 0) {
    return "0 B/s";
  }
  return `${formatBytes(rate)}/s`;
}

function formatEta(seconds) {
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return "—";
  }
  const total = Math.floor(seconds);
  const hrs = Math.floor(total / 3600);
  const mins = Math.floor((total % 3600) / 60);
  const secs = total % 60;
  if (hrs > 0) {
    return `${hrs}h ${mins}m`;
  }
  if (mins > 0) {
    return `${mins}m ${secs}s`;
  }
  return `${secs}s`;
}

function escapeHtml(value) {
  return (value || "").replace(/[&<>"']/g, (ch) => {
    switch (ch) {
      case "&":
        return "&amp;";
      case "<":
        return "&lt;";
      case ">":
        return "&gt;";
      case '"':
        return "&quot;";
      case "'":
        return "&#39;";
      default:
        return ch;
    }
  });
}

function clampNumber(value, min, max) {
  return Math.min(Math.max(Number(value) || 0, min), max);
}

function base64FromArrayBuffer(buffer) {
  const bytes = new Uint8Array(buffer);
  let binary = "";
  const chunkSize = 0x8000;
  for (let i = 0; i < bytes.length; i += chunkSize) {
    const chunk = bytes.subarray(i, i + chunkSize);
    binary += String.fromCharCode.apply(null, chunk);
  }
  return btoa(binary);
}

function syncSourceInputs() {
  const magnetFilled = elements.magnetInput.value.trim().length > 0;
  const urlFilled = elements.torrentUrlInput.value.trim().length > 0;
  const fileFilled = elements.fileInput.files && elements.fileInput.files.length > 0;

  const activeCount = [magnetFilled, urlFilled, fileFilled].filter(Boolean).length;
  const exclusiveMessage = activeCount > 1 ? "Only one source is allowed." : "";
  elements.sourceHint.textContent = exclusiveMessage || "Provide exactly one source: magnet link, torrent URL, or upload a .torrent file.";
  elements.sourceHint.classList.toggle("error", exclusiveMessage.length > 0);
  if (exclusiveMessage) {
    elements.sourceHint.setAttribute("role", "alert");
  } else {
    elements.sourceHint.removeAttribute("role");
  }

  const disableMagnet = (urlFilled || fileFilled) && !magnetFilled;
  const disableUrl = (magnetFilled || fileFilled) && !urlFilled;
  const disableFile = (magnetFilled || urlFilled) && !fileFilled;

  elements.magnetInput.disabled = disableMagnet;
  elements.torrentUrlInput.disabled = disableUrl;
  elements.fileInput.disabled = disableFile;

  if (elements.uploadTrigger) {
    elements.uploadTrigger.classList.toggle("disabled", disableFile);
  }

  if (elements.selectedFileName) {
    elements.selectedFileName.textContent = fileFilled
      ? `Selected file: ${elements.fileInput.files[0].name}`
      : "";
  }
}

function resetSourceInputs() {
  elements.magnetInput.disabled = false;
  elements.torrentUrlInput.disabled = false;
  elements.fileInput.disabled = false;
  elements.fileInput.value = "";
  if (elements.selectedFileName) {
    elements.selectedFileName.textContent = "";
  }
  elements.sourceHint.textContent = "Provide exactly one source: magnet link, torrent URL, or upload a .torrent file.";
  elements.sourceHint.classList.remove("error");
  syncSourceInputs();
}

function clearMessage() {
  elements.notificationBar.textContent = "";
  elements.notificationBar.className = "notification-bar";
  if (state.messageTimeout) {
    clearTimeout(state.messageTimeout);
    state.messageTimeout = null;
  }
}

function renderStats(stats) {
  elements.statTorrents.textContent = stats.totalTorrents ?? 0;
  elements.statPeers.textContent = stats.activePeers ?? 0;
  elements.statDownloaded.textContent = formatBytes(stats.bytesDownloaded ?? 0);
  elements.statUploaded.textContent = formatBytes(stats.bytesUploaded ?? 0);
}

function renderSystemResources(system) {
  if (!system) return;

  // CPU
  const cpuPercent = Math.min(Math.max(system.cpu?.usagePercent || 0, 0), 100);
  const cpuValue = document.getElementById('cpu-value');
  const cpuCores = document.getElementById('cpu-cores');
  if (cpuValue) cpuValue.textContent = `${cpuPercent.toFixed(1)}%`;
  if (cpuCores) cpuCores.textContent = `${system.cpu?.cores || 0} cores`;

  // RAM
  const ramPercent = Math.min(Math.max(system.memory?.usagePercent || 0, 0), 100);
  const ramValue = document.getElementById('ram-value');
  const ramDetail = document.getElementById('ram-detail');
  if (ramValue) ramValue.textContent = `${ramPercent.toFixed(1)}%`;
  if (ramDetail) {
    const usedGB = (system.memory?.used || 0) / (1024 ** 3);
    const totalGB = (system.memory?.total || 0) / (1024 ** 3);
    ramDetail.textContent = `${usedGB.toFixed(1)} / ${totalGB.toFixed(1)} GB`;
  }

  // Disk
  const diskPercent = Math.min(Math.max(system.disk?.usagePercent || 0, 0), 100);
  const diskValue = document.getElementById('disk-value');
  const diskDetail = document.getElementById('disk-detail');
  if (diskValue) diskValue.textContent = `${diskPercent.toFixed(1)}%`;
  if (diskDetail) {
    const downloadGB = (system.disk?.downloadDirUsed || 0) / (1024 ** 3);
    const usedGB = (system.disk?.used || 0) / (1024 ** 3);
    const totalGB = (system.disk?.total || 0) / (1024 ** 3);
    const freeGB = (system.disk?.free || 0) / (1024 ** 3);
    diskDetail.textContent = `${downloadGB.toFixed(1)}GB used | ${usedGB.toFixed(1)} / ${totalGB.toFixed(1)} GB partition | ${freeGB.toFixed(1)} GB free`;
  }

  // Network
  const netDown = document.getElementById('net-down');
  const netUp = document.getElementById('net-up');
  if (netDown) netDown.textContent = formatRate(system.network?.downloadRate || 0);
  if (netUp) netUp.textContent = formatRate(system.network?.uploadRate || 0);
}

function showDetailsLoading() {
  elements.detailsModal.hidden = false;
  elements.detailsContent.innerHTML = `
    <div class="details-loading" role="status" aria-live="polite">
      <span class="spinner" aria-hidden="true"></span>
      <span>Loading torrent details…</span>
    </div>
  `;
}

function setRowActive(infoHash, active) {
  if (!elements.tableBody) {
    return;
  }
  const detailsButton = elements.tableBody.querySelector(
    `button[data-action="details"][data-hash="${infoHash}"]`
  );
  if (!detailsButton) {
    return;
  }
  detailsButton.textContent = active ? "Hide" : "Details";
  const row = detailsButton.closest("tr");
  if (row) {
    row.classList.toggle("active-row", active);
  }
}

// Media Player Functions
function isVideoFile(filename) {
  const ext = filename.toLowerCase().substring(filename.lastIndexOf('.'));
  return VIDEO_EXTENSIONS.includes(ext);
}

function openMediaPlayer(fileUrl, fileName) {
  elements.mediaTitle.textContent = fileName || 'Media Player';
  elements.mediaSource.src = fileUrl;
  elements.mediaPlayer.load();
  elements.mediaModal.hidden = false;
  
  // Pause polling while playing video
  if (state.timer) {
    clearInterval(state.timer);
    state.timer = null;
  }
}

function closeMediaPlayer() {
  elements.mediaPlayer.pause();
  elements.mediaSource.src = '';
  elements.mediaModal.hidden = true;
  
  // Resume polling
  if (!state.timer) {
    state.timer = setInterval(refreshTorrents, POLL_INTERVAL_MS);
  }
}

// Handle play button clicks
document.addEventListener('click', (event) => {
  const playBtn = event.target.closest('.play-file-btn');
  if (playBtn && state.detailsHash) {
    const fileIndex = playBtn.dataset.fileIndex;
    const fileName = playBtn.dataset.filePath;
    const videoUrl = `${API_BASE}/torrents/${state.detailsHash}/files/${fileIndex}`;
    openMediaPlayer(videoUrl, fileName);
    return;
  }

  const deleteBtn = event.target.closest('.delete-file-btn');
  if (deleteBtn && state.detailsHash) {
    const fileIndex = deleteBtn.dataset.fileIndex;
    if (!confirm('Delete this incomplete file from server disk?')) {
      return;
    }
    apiRequest('DELETE', `${API_BASE}/torrents/${state.detailsHash}/files/${fileIndex}`)
      .then(async () => {
        showMessage('Incomplete file deleted.', false);
        await loadTorrentDetails(state.detailsHash, { silent: true, keepPanel: true });
        await refreshTorrents({ skipDetailUpdate: true });
      })
      .catch((error) => {
        showMessage(error.message, true);
      });
  }
});

// Close modal on background click
elements.mediaModal?.addEventListener('click', (event) => {
  if (event.target === elements.mediaModal) {
    closeMediaPlayer();
  }
});

elements.selectionModal?.addEventListener('click', (event) => {
  if (event.target === elements.selectionModal) {
    closeFileSelectionModal();
  }
});

// File Selection Modal Functions
let currentSelectionTorrent = null;

function showFileSelectionModal(torrentData) {
  currentSelectionTorrent = torrentData;
  elements.selectionTitle.textContent = `Select Files - ${torrentData.name || torrentData.infoHash}`;
  
  const filesHtml = torrentData.files.map(file => `
    <label class="selection-file-item">
      <input type="checkbox" data-file-index="${file.index}" checked>
      <div class="selection-file-info">
        <div class="selection-file-name">${escapeHtml(file.path)}</div>
        <div class="selection-file-size">${formatBytes(file.length)}</div>
      </div>
    </label>
  `).join('');
  
  elements.selectionFilesList.innerHTML = filesHtml;
  elements.selectionModal.hidden = false;
}

function closeFileSelectionModal() {
  elements.selectionModal.hidden = true;
  currentSelectionTorrent = null;
  elements.selectionFilesList.innerHTML = '';
}

function toggleAllFileCheckboxes(checked) {
  const checkboxes = elements.selectionFilesList.querySelectorAll('input[type="checkbox"]');
  checkboxes.forEach(cb => cb.checked = checked);
}

async function confirmFileSelection() {
  if (!currentSelectionTorrent) return;
  
  const checkboxes = elements.selectionFilesList.querySelectorAll('input[type="checkbox"]');
  const selectedIndices = [];
  
  checkboxes.forEach(cb => {
    if (cb.checked) {
      selectedIndices.push(parseInt(cb.dataset.fileIndex));
    }
  });
  
  if (selectedIndices.length === 0) {
    showMessage("Please select at least one file.", true);
    return;
  }
  
  try {
    const payload = {
      applySelection: true,
      selectedFiles: selectedIndices
    };
    
    await apiRequest("POST", `${API_BASE}/torrents/${currentSelectionTorrent.infoHash}/selection`, payload);
    closeFileSelectionModal();
    showMessage(`Starting download of ${selectedIndices.length} file(s).`, false);
    await refreshTorrents();
  } catch (error) {
    showMessage(error.message, true);
  }
}

async function downloadAllFiles() {
  if (!currentSelectionTorrent) return;
  
  try {
    const payload = {
      applySelection: false
    };
    
    await apiRequest("POST", `${API_BASE}/torrents/${currentSelectionTorrent.infoHash}/selection`, payload);
    closeFileSelectionModal();
    showMessage("Downloading all files.", false);
    await refreshTorrents();
  } catch (error) {
    showMessage(error.message, true);
  }
}
