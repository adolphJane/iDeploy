package agent

import (
	"math"
	"testing"
)

func TestCPUUsage(t *testing.T) {
	previous, err := parseCPU("cpu  100 0 50 850 0 0 0 0")
	if err != nil {
		t.Fatal(err)
	}
	current, err := parseCPU("cpu  150 0 70 930 0 0 0 0")
	if err != nil {
		t.Fatal(err)
	}
	usage, ok := cpuUsage(previous, current)
	if !ok || math.Abs(usage-46.666666666666664) > 0.000001 {
		t.Fatalf("unexpected usage: %v %v", usage, ok)
	}
}

func TestMemoryAvailable(t *testing.T) {
	got, err := parseMemory("MemTotal: 1000 kB\nMemAvailable: 400 kB\nSwapTotal: 200 kB\nSwapFree: 100 kB\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedBytes != 600*1024 || got.AvailableBytes != 400*1024 || got.SwapFreeBytes != 100*1024 {
		t.Fatalf("unexpected memory: %+v", got)
	}
}
