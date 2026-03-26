package pool

import (
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

const reservedForNonGo = 64 * 1024 * 1024

func InitProcessEnv() {
	setMemoryLimit()
	setCPUCount()
}

func setMemoryLimit() {
	memLimit := getContainerMemoryLimit()
	if memLimit <= 0 {
		return
	}

	goLimit := memLimit - reservedForNonGo

	const minGoMemory = 32 * 1024 * 1024
	if goLimit < minGoMemory {
		goLimit = minGoMemory
	}

	debug.SetMemoryLimit(goLimit)
}

func setCPUCount() {
	cpuLimit := getContainerCPULimit()
	if cpuLimit <= 0 {
		return
	}

	n := int(math.Ceil(cpuLimit))
	maxProcs := min(n, runtime.NumCPU())
	if maxProcs > 0 {
		runtime.GOMAXPROCS(maxProcs)
	}
}

func getContainerCPULimit() float64 {
	if data, err := os.ReadFile("/sys/fs/cgroup/cpu.max"); err == nil {
		parts := strings.Fields(string(data))
		if len(parts) == 2 && parts[0] != "max" {
			quota, _ := strconv.ParseFloat(parts[0], 64)
			period, _ := strconv.ParseFloat(parts[1], 64)
			if period > 0 {
				return quota / period
			}
		}
	}

	quota := readFloatFromFile("/sys/fs/cgroup/cpu/cpu.cfs_quota_us")
	period := readFloatFromFile("/sys/fs/cgroup/cpu/cpu.cfs_period_us")
	if quota > 0 && period > 0 {
		return quota / period
	}

	return 0
}

func getContainerMemoryLimit() int64 {
	if v := readIntFromFile("/sys/fs/cgroup/memory.max"); v > 0 {
		return v
	}
	if v := readIntFromFile("/sys/fs/cgroup/memory/memory.limit_in_bytes"); v > 0 && v < (1<<62) {
		return v
	}
	return 0
}

func readIntFromFile(path string) int64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	s := strings.TrimSpace(string(data))
	if s == "max" {
		return -1
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func readFloatFromFile(path string) float64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	s := strings.TrimSpace(string(data))
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
