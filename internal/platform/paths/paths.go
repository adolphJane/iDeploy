package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const appDirectoryName = "iDeploy"

// AppPaths contains every writable location used by the desktop application.
type AppPaths struct {
	DataDir      string `json:"dataDir"`
	CacheDir     string `json:"cacheDir"`
	LogDir       string `json:"logDir"`
	DatabasePath string `json:"databasePath"`
}

// Resolve returns platform-appropriate per-user paths without creating them.
func Resolve() (AppPaths, error) {
	dataRoot, err := os.UserConfigDir()
	if err != nil {
		return AppPaths{}, fmt.Errorf("resolve user data directory: %w", err)
	}

	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return AppPaths{}, fmt.Errorf("resolve user cache directory: %w", err)
	}

	dataDir := filepath.Join(dataRoot, appDirectoryName)
	cacheDir := filepath.Join(cacheRoot, appDirectoryName)
	logDir := filepath.Join(dataDir, "logs")

	if runtime.GOOS == "darwin" {
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			return AppPaths{}, fmt.Errorf("resolve user home directory: %w", homeErr)
		}
		logDir = filepath.Join(homeDir, "Library", "Logs", appDirectoryName)
	}

	return AppPaths{
		DataDir:      dataDir,
		CacheDir:     cacheDir,
		LogDir:       logDir,
		DatabasePath: filepath.Join(dataDir, "ideploy.db"),
	}, nil
}

// Ensure creates the application directories with permissions limited to the
// current operating-system user.
func Ensure(appPaths AppPaths) error {
	for _, directory := range []string{appPaths.DataDir, appPaths.CacheDir, appPaths.LogDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create application directory %q: %w", directory, err)
		}
	}
	return nil
}
