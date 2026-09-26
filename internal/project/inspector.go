package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type Inspection struct {
	Status         Status
	Detail         string
	DockerfilePath string
	ComposePath    string
}

// InspectLocal performs a deliberately bounded source scan. It never follows
// symlinked directories or enters dependency and VCS folders, keeping a UI
// initiated check fast even for large workspaces.
func InspectLocal(root string) (Inspection, error) {
	info, err := os.Stat(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Inspection{Status: StatusUnavailable, Detail: "本地目录不存在"}, nil
		}
		return Inspection{Status: StatusFailed, Detail: err.Error()}, nil
	}
	if !info.IsDir() {
		return Inspection{Status: StatusUnavailable, Detail: "选择的路径不是目录"}, nil
	}

	result := Inspection{Status: StatusNeedsConfiguration, Detail: "未发现 Dockerfile，请在构建配置中指定"}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		depth := strings.Count(filepath.ToSlash(relative), "/") + 1
		if entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 || skippedDirectory(entry.Name()) || depth > 3 {
				return filepath.SkipDir
			}
			return nil
		}
		if depth > 3 || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		name := strings.ToLower(entry.Name())
		relative = filepath.ToSlash(relative)
		if result.DockerfilePath == "" && (name == "dockerfile" || strings.HasPrefix(name, "dockerfile.")) {
			result.DockerfilePath = relative
		}
		if result.ComposePath == "" && (name == "docker-compose.yml" || name == "docker-compose.yaml" || name == "compose.yml" || name == "compose.yaml") {
			result.ComposePath = relative
		}
		return nil
	})
	if err != nil {
		return Inspection{Status: StatusFailed, Detail: fmt.Sprintf("扫描目录失败：%v", err)}, nil
	}
	if result.DockerfilePath != "" {
		result.Status = StatusReady
		result.Detail = "已识别 Docker 构建配置"
	} else if result.ComposePath != "" {
		result.Detail = "已识别 Compose 文件；请补充 Dockerfile 或构建配置"
	}
	return result, nil
}

func skippedDirectory(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", ".idea", ".vscode", "dist", "build":
		return true
	default:
		return false
	}
}
