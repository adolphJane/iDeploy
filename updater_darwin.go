//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
void ideploy_start_app_updater(void);
int ideploy_check_for_updates(void);
int ideploy_automatic_update_checks(void);
void ideploy_set_automatic_update_checks(int enabled);
char *ideploy_app_update_progress_json(void);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"
)

// Sparkle is loaded from the packaged app at runtime so CLI and development
// builds do not need a downloaded framework to compile or start.
func startAppUpdater() {
	C.ideploy_start_app_updater()
}

func checkForAppUpdates() bool {
	return C.ideploy_check_for_updates() != 0
}

func (s *SystemService) CheckForAppUpdates() bool {
	return checkForAppUpdates()
}

func (s *SystemService) GetAutomaticUpdateChecks() bool {
	return C.ideploy_automatic_update_checks() != 0
}

func (s *SystemService) SetAutomaticUpdateChecks(enabled bool) bool {
	value := C.int(0)
	if enabled {
		value = 1
	}
	C.ideploy_set_automatic_update_checks(value)
	return s.GetAutomaticUpdateChecks()
}

func sparkleUpdateProgress() (AppUpdateProgress, error) {
	value := C.ideploy_app_update_progress_json()
	if value == nil {
		return AppUpdateProgress{}, errors.New("无法读取 Sparkle 更新进度")
	}
	defer C.free(unsafe.Pointer(value))
	var progress AppUpdateProgress
	if err := json.Unmarshal([]byte(C.GoString(value)), &progress); err != nil {
		return AppUpdateProgress{}, err
	}
	progress.Percent = -1
	return progress, nil
}
