package releaseplan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Document is the non-secret, versioned build instruction committed to Git.
// Credentials and environment values are resolved by the service at execution time.
type Document struct {
	SchemaVersion int    `json:"schemaVersion"`
	DeploymentID  string `json:"deploymentId"`
	ReleaseID     string `json:"releaseId"`
	SourceCommit  string `json:"sourceCommit"`
	WorkloadKind  string `json:"workloadKind"`
	ContextPath   string `json:"contextPath"`
	Dockerfile    string `json:"dockerfile,omitempty"`
	ComposeFile   string `json:"composeFile,omitempty"`
	ImagePath     string `json:"imagePath,omitempty"`
	Platform      string `json:"platform"`
	BasePath      string `json:"basePath,omitempty"`
}

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

func (d Document) Validate() error {
	if d.SchemaVersion != 1 || !idPattern.MatchString(d.DeploymentID) || !idPattern.MatchString(d.ReleaseID) || !commitPattern.MatchString(d.SourceCommit) {
		return errors.New("发布计划版本或标识无效")
	}
	if d.WorkloadKind != "container" && d.WorkloadKind != "compose" {
		return errors.New("发布计划工作负载类型无效")
	}
	if !safeRelative(d.ContextPath) || d.ContextPath == "" || !safeRelative(d.Dockerfile) || !safeRelative(d.ComposeFile) {
		return errors.New("发布计划文件路径必须位于源码目录内")
	}
	if d.WorkloadKind == "container" && (d.Dockerfile == "" || d.ImagePath == "") || d.WorkloadKind == "compose" && d.ComposeFile == "" {
		return errors.New("发布计划缺少构建配置")
	}
	if d.Platform != "linux/amd64" && d.Platform != "linux/arm64" {
		return errors.New("发布计划平台无效")
	}
	if strings.ContainsAny(d.BasePath, "\r\n\x00") || len(d.BasePath) > 256 {
		return errors.New("应用路径无效")
	}
	if strings.ContainsAny(d.ImagePath, "\r\n\x00 ") {
		return errors.New("镜像仓库路径无效")
	}
	return nil
}

func safeRelative(value string) bool {
	if value == "" {
		return true
	}
	if strings.ContainsAny(value, "\\\r\n\x00") || strings.HasPrefix(value, "/") {
		return false
	}
	clean := path.Clean(value)
	return clean == value && clean != ".." && !strings.HasPrefix(clean, "../")
}

func (d Document) Encode() (name string, data []byte, digest string, err error) {
	if err = d.Validate(); err != nil {
		return "", nil, "", err
	}
	data, err = json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", nil, "", err
	}
	data = append(data, '\n')
	sum := sha256.Sum256(data)
	return fmt.Sprintf(".ideploy/releases/%s/%s.json", d.DeploymentID, d.ReleaseID), data, hex.EncodeToString(sum[:]), nil
}
