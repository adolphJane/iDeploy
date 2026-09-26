package toolchain

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

type Source string

const (
	SourceProcessPath Source = "process_path"
	SourceZshStartup  Source = "zsh_startup"
)

type Executable struct {
	Path   string
	Source Source
}

// ResolveExecutable first checks the environment inherited by the macOS app.
// Finder-launched applications usually have a minimal PATH, so on macOS it
// falls back to an interactive login zsh. That mode loads both .zprofile and
// .zshrc, matching the environment users see in Terminal.
func ResolveExecutable(ctx context.Context, name string) (Executable, error) {
	if !safeExecutableName(name) {
		return Executable{}, fmt.Errorf("invalid executable name %q", name)
	}

	if resolved, err := exec.LookPath(name); err == nil {
		return Executable{Path: resolved, Source: SourceProcessPath}, nil
	}

	if runtime.GOOS != "darwin" {
		return Executable{}, fmt.Errorf("executable %q was not found in PATH", name)
	}

	resolved, err := resolveFromZsh(ctx, name)
	if err != nil {
		return Executable{}, fmt.Errorf("resolve %q from zsh startup files: %w", name, err)
	}
	return Executable{Path: resolved, Source: SourceZshStartup}, nil
}

func resolveFromZsh(parent context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()

	// The name is validated above. The marker lets us ignore output produced by
	// shell plugins while .zshrc is loading.
	script := fmt.Sprintf("printf '__IDEPLOY_PATH__=%%s\\n' \"${commands[%s]:-}\"", name)
	output, err := exec.CommandContext(ctx, "/bin/zsh", "-lic", script).Output()
	if ctx.Err() != nil {
		return "", errors.New("zsh startup timed out")
	}
	if err != nil {
		return "", err
	}

	resolved := parseMarkedPath(string(output))
	if resolved == "" {
		return "", errors.New("not found after loading .zprofile and .zshrc")
	}
	if !filepath.IsAbs(resolved) {
		return "", fmt.Errorf("zsh returned a non-absolute path %q", resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect resolved path: %w", err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return "", fmt.Errorf("resolved path %q is not executable", resolved)
	}
	return resolved, nil
}

func parseMarkedPath(output string) string {
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if value, found := strings.CutPrefix(line, "__IDEPLOY_PATH__="); found {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safeExecutableName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || strings.ContainsRune("._-", character) {
			continue
		}
		return false
	}
	return true
}
