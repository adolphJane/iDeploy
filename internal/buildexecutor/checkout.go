package buildexecutor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"ideploy/internal/releaseplan"
)

var idPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type CheckoutInput struct {
	RepositoryURL  string
	GitEnvironment []string
	DeliveryCommit string
	DeliveryDigest string
	DeploymentID   string
	ReleaseID      string
	WorkRoot       string
}

type Checkout struct {
	Root     string
	Document releaseplan.Document
}

// Prepare materializes an exact delivery commit in an isolated directory.
// The caller must supply a validated repository URL and task-scoped Git auth.
// No fallback to a moving branch is permitted.
func Prepare(ctx context.Context, input CheckoutInput) (Checkout, error) {
	if !idPattern.MatchString(input.DeploymentID) || !idPattern.MatchString(input.ReleaseID) ||
		!commitPattern.MatchString(input.DeliveryCommit) || !digestPattern.MatchString(input.DeliveryDigest) {
		return Checkout{}, errors.New("构建任务标识无效")
	}
	if !filepath.IsAbs(input.WorkRoot) || input.RepositoryURL == "" || strings.ContainsAny(input.RepositoryURL, "\r\n\x00") {
		return Checkout{}, errors.New("构建工作区或仓库地址无效")
	}
	base := filepath.Join(input.WorkRoot, input.DeploymentID)
	if err := os.MkdirAll(base, 0700); err != nil {
		return Checkout{}, err
	}
	baseInfo, err := os.Lstat(base)
	if err != nil || !baseInfo.IsDir() {
		return Checkout{}, errors.New("构建目录不是普通目录")
	}
	root := filepath.Join(base, input.ReleaseID)
	if _, err := os.Lstat(root); err == nil {
		return Checkout{}, errors.New("发布工作区已存在，请先对账旧任务")
	} else if !os.IsNotExist(err) {
		return Checkout{}, err
	}
	if err := runGitEnv(ctx, "", input.GitEnvironment, "clone", "--no-checkout", "--", input.RepositoryURL, root); err != nil {
		return Checkout{}, errors.New("服务端无法拉取 Git 仓库")
	}
	valid := false
	defer func() {
		if !valid {
			_ = os.RemoveAll(root)
		}
	}()
	if err := runGit(ctx, root, "checkout", "--detach", input.DeliveryCommit); err != nil {
		return Checkout{}, errors.New("服务端无法检出发布提交")
	}
	actual, err := gitOutput(ctx, root, "rev-parse", "HEAD")
	if err != nil || actual != input.DeliveryCommit {
		return Checkout{}, errors.New("检出的 Git 提交与发布计划不一致")
	}
	name := filepath.Join(root, ".ideploy", "releases", input.DeploymentID, input.ReleaseID+".json")
	for current := name; current != root; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 {
			return Checkout{}, errors.New("部署文件路径包含符号链接或缺失项")
		}
	}
	data, err := os.ReadFile(name)
	if err != nil || len(data) > 64*1024 {
		return Checkout{}, errors.New("发布提交缺少有效部署文件")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != input.DeliveryDigest {
		return Checkout{}, errors.New("部署文件摘要与任务不一致")
	}
	var document releaseplan.Document
	if err := json.Unmarshal(data, &document); err != nil || document.Validate() != nil || document.DeploymentID != input.DeploymentID || document.ReleaseID != input.ReleaseID {
		return Checkout{}, errors.New("部署文件内容无效")
	}
	parent, err := gitOutput(ctx, root, "rev-parse", "HEAD^")
	if err != nil || parent != document.SourceCommit {
		return Checkout{}, errors.New("发布提交的源码父提交不匹配")
	}
	valid = true
	return Checkout{Root: root, Document: document}, nil
}

func gitOutput(ctx context.Context, directory string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func runGit(ctx context.Context, directory string, args ...string) error {
	_, err := gitOutput(ctx, directory, args...)
	return err
}

func runGitEnv(ctx context.Context, directory string, environment []string, args ...string) error {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	if environment != nil {
		command.Env = environment
	}
	return command.Run()
}
