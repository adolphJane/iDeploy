package agent

import (
	"bufio"
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type CPU struct {
	Status       string     `json:"status"`
	UsagePercent float64    `json:"usagePercent"`
	LogicalCores int        `json:"logicalCores"`
	Load         [3]float64 `json:"load"`
	Error        string     `json:"error,omitempty"`
}

type Memory struct {
	Status         string `json:"status"`
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
	UsedBytes      uint64 `json:"usedBytes"`
	SwapTotalBytes uint64 `json:"swapTotalBytes"`
	SwapFreeBytes  uint64 `json:"swapFreeBytes"`
	Error          string `json:"error,omitempty"`
}

type Storage struct {
	Status         string `json:"status"`
	MountPoint     string `json:"mountPoint"`
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
	UsedBytes      uint64 `json:"usedBytes"`
	Error          string `json:"error,omitempty"`
}

type Metrics struct {
	ObservedAt string    `json:"observedAt"`
	CPU        CPU       `json:"cpu"`
	Memory     Memory    `json:"memory"`
	Storage    []Storage `json:"storage"`
}

type cpuCounters struct{ total, idle uint64 }

func parseCPU(line string) (cpuCounters, error) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuCounters{}, errors.New("invalid /proc/stat cpu line")
	}
	var result cpuCounters
	for i, field := range fields[1:] {
		value, err := strconv.ParseUint(field, 10, 64)
		if err != nil {
			return cpuCounters{}, err
		}
		result.total += value
		if i == 3 || i == 4 {
			result.idle += value
		}
	}
	return result, nil
}

func cpuUsage(previous, current cpuCounters) (float64, bool) {
	if current.total <= previous.total || current.idle < previous.idle {
		return 0, false
	}
	delta := current.total - previous.total
	if current.idle-previous.idle > delta {
		return 0, false
	}
	busy := delta - (current.idle - previous.idle)
	return float64(busy) * 100 / float64(delta), true
}

func readCPU() (cpuCounters, [3]float64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuCounters{}, [3]float64{}, err
	}
	first := strings.SplitN(string(data), "\n", 2)[0]
	counters, err := parseCPU(first)
	if err != nil {
		return cpuCounters{}, [3]float64{}, err
	}
	loadData, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return counters, [3]float64{}, err
	}
	fields := strings.Fields(string(loadData))
	if len(fields) < 3 {
		return counters, [3]float64{}, errors.New("invalid /proc/loadavg")
	}
	var load [3]float64
	for i := range load {
		load[i], err = strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return counters, [3]float64{}, err
		}
	}
	return counters, load, nil
}

func parseMemory(input string) (Memory, error) {
	values := map[string]uint64{}
	for _, line := range strings.Split(input, "\n") {
		parts := strings.Fields(line)
		if len(parts) < 3 || parts[2] != "kB" {
			continue
		}
		value, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return Memory{}, err
		}
		values[strings.TrimSuffix(parts[0], ":")] = value * 1024
	}
	total, okTotal := values["MemTotal"]
	available, okAvailable := values["MemAvailable"]
	if !okTotal || !okAvailable || available > total {
		return Memory{}, errors.New("invalid /proc/meminfo")
	}
	return Memory{Status: "ok", TotalBytes: total, AvailableBytes: available, UsedBytes: total - available,
		SwapTotalBytes: values["SwapTotal"], SwapFreeBytes: values["SwapFree"]}, nil
}

func readMemory() Memory {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return Memory{Status: "error", Error: err.Error()}
	}
	value, err := parseMemory(string(data))
	if err != nil {
		return Memory{Status: "error", Error: err.Error()}
	}
	return value
}

func mountFor(path string) (string, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", err
	}
	defer file.Close()
	best := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 5 {
			continue
		}
		mount := strings.ReplaceAll(fields[4], `\040`, " ")
		if (path == mount || strings.HasPrefix(path, strings.TrimRight(mount, "/")+"/")) && len(mount) > len(best) {
			best = mount
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if best == "" {
		return "", errors.New("mount not found")
	}
	return best, nil
}

func readStorage(dockerRoot string) []Storage {
	paths := []string{"/"}
	if dockerRoot != "" {
		paths = append(paths, dockerRoot)
	}
	seen := map[string]bool{}
	result := []Storage{}
	for _, path := range paths {
		mount, err := mountFor(path)
		if err != nil {
			result = append(result, Storage{Status: "error", MountPoint: path, Error: err.Error()})
			continue
		}
		if seen[mount] {
			continue
		}
		seen[mount] = true
		var stat syscall.Statfs_t
		if err := syscall.Statfs(mount, &stat); err != nil {
			result = append(result, Storage{Status: "error", MountPoint: mount, Error: err.Error()})
			continue
		}
		total := stat.Blocks * uint64(stat.Bsize)
		available := stat.Bavail * uint64(stat.Bsize)
		result = append(result, Storage{Status: "ok", MountPoint: mount, TotalBytes: total, AvailableBytes: available, UsedBytes: total - available})
	}
	return result
}

type Sampler struct {
	previous    cpuCounters
	hasPrevious bool
	latest      Metrics
}

func (s *Sampler) SampleSnapshot() Metrics { return s.latest }

func (s *Sampler) Sample(dockerRoot string) Metrics {
	result := Metrics{ObservedAt: time.Now().UTC().Format(time.RFC3339), CPU: CPU{Status: "warming_up", LogicalCores: runtime.NumCPU()}, Memory: readMemory(), Storage: readStorage(dockerRoot)}
	counters, load, err := readCPU()
	if err != nil {
		result.CPU.Status = "error"
		result.CPU.Error = err.Error()
	} else {
		result.CPU.Load = load
		if s.hasPrevious {
			if usage, ok := cpuUsage(s.previous, counters); ok {
				result.CPU.Status = "ok"
				result.CPU.UsagePercent = usage
			}
		}
		s.previous, s.hasPrevious = counters, true
	}
	s.latest = result
	return result
}
