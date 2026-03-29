package server

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// SystemResources holds system resource information
type SystemResources struct {
	CPU        CPUInfo     `json:"cpu"`
	Memory     MemoryInfo  `json:"memory"`
	Disk       DiskInfo    `json:"disk"`
	Network    NetworkInfo `json:"network"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

type CPUInfo struct {
	UsagePercent float64 `json:"usagePercent"`
	Cores        int     `json:"cores"`
}

type MemoryInfo struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Available   uint64  `json:"available"`
	UsagePercent float64 `json:"usagePercent"`
}

type DiskInfo struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsagePercent float64 `json:"usagePercent"`
	DownloadDirUsed uint64  `json:"downloadDirUsed"`
	DownloadDirUsagePercent float64 `json:"downloadDirUsagePercent"`
	CacheInfo   CacheInfo `json:"cacheInfo"`
}

type CacheInfo struct {
	ZipCacheSize  uint64 `json:"zipCacheSize"`
	ZipCacheCount int    `json:"zipCacheCount"`
	OtherCacheSize uint64 `json:"otherCacheSize"`
	OtherCacheCount int    `json:"otherCacheCount"`
}

type NetworkInfo struct {
	UploadRate   float64 `json:"uploadRate"`   // bytes per second
	DownloadRate float64 `json:"downloadRate"` // bytes per second
	TotalSent    uint64  `json:"totalSent"`
	TotalRecv    uint64  `json:"totalRecv"`
}

var (
	lastNetStats      []net.IOCountersStat
	lastNetStatsTime  time.Time
	netStatsMu        sync.Mutex

	lastDirSizePath  string
	lastDirSizeValue uint64
	lastDirSizeAt    time.Time
	dirSizeMu        sync.Mutex
)

// GetSystemResources collects current system resource information
func GetSystemResources(dataDir string) (*SystemResources, error) {
	resources := &SystemResources{
		UpdatedAt: time.Now(),
	}

	// CPU info
	cpuPercent, err := cpu.Percent(0, false)
	if err == nil && len(cpuPercent) > 0 {
		resources.CPU.UsagePercent = cpuPercent[0]
	}
	resources.CPU.Cores = runtime.NumCPU()

	// Memory info
	memInfo, err := mem.VirtualMemory()
	if err == nil {
		resources.Memory.Total = memInfo.Total
		resources.Memory.Used = memInfo.Used
		resources.Memory.Available = memInfo.Available
		resources.Memory.UsagePercent = memInfo.UsedPercent
	}

	// Disk info for data directory
	diskInfo, err := disk.Usage(dataDir)
	if err == nil {
		resources.Disk.Total = diskInfo.Total
		resources.Disk.Used = diskInfo.Used
		resources.Disk.Free = diskInfo.Free
		resources.Disk.UsagePercent = diskInfo.UsedPercent

		dirSize, dirErr := getDirectorySizeCached(dataDir)
		if dirErr == nil {
			resources.Disk.DownloadDirUsed = dirSize
			if diskInfo.Total > 0 {
				resources.Disk.DownloadDirUsagePercent = float64(dirSize) / float64(diskInfo.Total) * 100
			}
		}

		// Get cache info
		resources.Disk.CacheInfo = getCacheStats()
	}

	// Network info
	netInfo, err := getNetworkStats()
	if err == nil {
		resources.Network = *netInfo
	}

	return resources, nil
}

func getDirectorySizeCached(dir string) (uint64, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	dirSizeMu.Lock()
	if abs == lastDirSizePath && now.Sub(lastDirSizeAt) < 15*time.Second {
		cached := lastDirSizeValue
		dirSizeMu.Unlock()
		return cached, nil
	}
	dirSizeMu.Unlock()

	size, err := calculateDirectorySize(abs)
	if err != nil {
		return 0, err
	}

	dirSizeMu.Lock()
	lastDirSizePath = abs
	lastDirSizeValue = size
	lastDirSizeAt = now
	dirSizeMu.Unlock()

	return size, nil
}

func calculateDirectorySize(root string) (uint64, error) {
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var total uint64
	blockSize := int64(4096) // default block size on most Linux systems

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}

		info, err := d.Info()
		if err != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return err
		}

		// Count all files and directories (directories also take space)
		if info.IsDir() {
			total += uint64(blockSize) // account for directory entry
		} else if size := info.Size(); size > 0 {
			// Calculate actual blocks used (rounds up to nearest block)
			blocks := (size + blockSize - 1) / blockSize
			total += uint64(blocks) * uint64(blockSize)
		}

		return nil
	})

	return total, err
}

func getNetworkStats() (*NetworkInfo, error) {
	netStatsMu.Lock()
	defer netStatsMu.Unlock()

	counters, err := net.IOCounters(false)
	if err != nil || len(counters) == 0 {
		return &NetworkInfo{}, err
	}

	current := counters[0]
	now := time.Now()

	info := &NetworkInfo{
		TotalSent: current.BytesSent,
		TotalRecv: current.BytesRecv,
	}

	// Calculate rates if we have previous stats
	if lastNetStats != nil && len(lastNetStats) > 0 && !lastNetStatsTime.IsZero() {
		elapsed := now.Sub(lastNetStatsTime).Seconds()
		if elapsed > 0 {
			prev := lastNetStats[0]
			info.UploadRate = float64(current.BytesSent-prev.BytesSent) / elapsed
			info.DownloadRate = float64(current.BytesRecv-prev.BytesRecv) / elapsed
		}
	}

	// Store current stats for next calculation
	lastNetStats = counters
	lastNetStatsTime = now

	return info, nil
}

func getCacheStats() CacheInfo {
	tmpDir := os.TempDir()
	info := CacheInfo{}

	// Check tordown-zip-cache directory
	cacheDir := filepath.Join(tmpDir, "tordown-zip-cache")
	if entries, err := os.ReadDir(cacheDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if fileInfo, err := entry.Info(); err == nil {
				info.ZipCacheSize += uint64(fileInfo.Size())
				info.ZipCacheCount++
			}
		}
	}

	// Check root tmp directory for tordown-*.zip files
	if entries, err := os.ReadDir(tmpDir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, "tordown-") && strings.HasSuffix(name, ".zip") {
				if fileInfo, err := entry.Info(); err == nil {
					info.OtherCacheSize += uint64(fileInfo.Size())
					info.OtherCacheCount++
				}
			}
		}
	}

	return info
}
