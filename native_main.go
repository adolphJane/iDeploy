//go:build native

package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"ideploy/internal/platform/instance"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "native-backend" {
		runNativeBackend()
		return
	}
	if shouldRunMCP(os.Args[1:]) {
		os.Exit(runMCPCommand(os.Stderr))
	}
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runNativeBackend() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	parentPID := os.Getppid()
	if len(os.Args) > 2 {
		if value, err := strconv.Atoi(os.Args[2]); err == nil && value > 1 {
			parentPID = value
		}
	}
	service := NewSystemService()
	for attempt := 0; ; attempt++ {
		err := service.start(ctx, true)
		if err == nil {
			break
		}
		// A replaced app can launch before the previous backend releases its
		// advisory runtime lock. Give that process a brief chance to exit.
		if !errors.Is(err, instance.ErrLocked) || attempt >= 29 {
			log.Fatal(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
	defer service.close()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if os.Getppid() != parentPID {
				return
			}
		}
	}
}
