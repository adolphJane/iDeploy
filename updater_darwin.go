//go:build darwin

package main

/*
#cgo LDFLAGS: -framework Foundation
void ideploy_start_app_updater(void);
*/
import "C"

// Sparkle is loaded from the packaged app at runtime so CLI and development
// builds do not need a downloaded framework to compile or start.
func startAppUpdater() {
	C.ideploy_start_app_updater()
}
