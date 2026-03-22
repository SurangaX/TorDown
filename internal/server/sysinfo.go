package server

import (
	"runtime"
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
	}

	// Network info
	netInfo, err := getNetworkStats()
	if err == nil {
		resources.Network = *netInfo
	}

	return resources, nil
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
