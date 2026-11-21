package gitdelivery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"ideploy/internal/releaseplan"
)

var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

type Result struct {
	SourceCommit   string
	DeliveryCommit string
	DeliveryRef    string
	DocumentPath   string
	Digest         string
}

// Push creates a single immutable release branch from an exact source commit.
// Git credentials are supplied by the caller's Git environment; they are never
// embedded in the repository URL or returned in command errors.
func Push(ctx context.Context, repositoryURL, sourceCommit string, document releaseplan.Document, cacheDir string, gitEnvironment ...string) (Result, error) {
	if !commitPattern.MatchString(sourceCommit) || document.SourceCommit != sourceCommit {
		return Result{}, errors.New("源码提交必须是与发布文件一致的完整 Git SHA")
	}
	name, contents, digest, err := document.Encode()
	if err != nil {
		return Result{}, err
	}
	if repositoryURL == "" || strings.ContainsAny(repositoryURL, "\r\n\x00") {
		return Result{}, errors.New("Git 仓库地址无效")
	}
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return Result{}, err
	}
	root, err := os.MkdirTemp(cacheDir, "delivery-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(root)
	if _, err = git(ctx, "", gitEnvironment, "clone", "--no-checkout", "--", repositoryURL, root); err != nil {
		return Result{}, errors.New("无法检出 Git 仓库，请检查地址和凭据")
	}
	if _, err = git(ctx, root, nil, "checkout", "--detach", sourceCommit); err != nil {
		return Result{}, errors.New("指定源码提交不在 Git 仓库中")
	}
	file := filepath.Join(root, filepath.FromSlash(name))
	for parent := filepath.Dir(file); parent != root; parent = filepath.Dir(parent) {
		info, statErr := os.Lstat(parent)
		if statErr == nil && !info.IsDir() {
			return Result{}, errors.New("发布文件路径包含非目录或符号链接")
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return Result{}, statErr
		}
	}
	if _, err = os.Lstat(file); err == nil {
		return Result{}, errors.New("仓库中已存在同名发布文件，已停止覆盖")
	} else if !os.IsNotExist(err) {
		return Result{}, err
	}
	if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		return Result{}, err
	}
	if err = os.WriteFile(file, contents, 0600); err != nil {
		return Result{}, err
	}
	if _, err = git(ctx, root, nil, "add", "--", name); err != nil {
		return Result{}, err
	}
	identity := []string{"GIT_AUTHOR_NAME=iDeploy", "GIT_AUTHOR_EMAIL=ideploy@localhost", "GIT_COMMITTER_NAME=iDeploy", "GIT_COMMITTER_EMAIL=ideploy@localhost"}
	if _, err = git(ctx, root, identity, "commit", "-m", "iDeploy release "+document.ReleaseID, "--", name); err != nil {
		return Result{}, errors.New("创建发布提交失败")
	}
	deliveryCommit, err := git(ctx, root, nil, "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	ref := fmt.Sprintf("refs/heads/ideploy/releases/%s/%s", document.DeploymentID, document.ReleaseID)
	probe := exec.CommandContext(ctx, "git", "ls-remote", "--exit-code", "--heads", "origin", ref)
	probe.Dir = root
	probe.Env = append(os.Environ(), gitEnvironment...)
	if err := probe.Run(); err == nil {
		return Result{}, errors.New("发布引用已存在，不能覆盖")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		return Result{}, errors.New("无法确认发布引用是否已存在")
	}
	if _, err = git(ctx, root, gitEnvironment, "push", "--force-with-lease="+ref+":", "origin", "HEAD:"+ref); err != nil {
		return Result{}, errors.New("推送专用发布引用失败，请检查写入权限及同名引用")
	}
	return Result{SourceCommit: sourceCommit, DeliveryCommit: deliveryCommit, DeliveryRef: ref, DocumentPath: name, Digest: digest}, nil
}

func git(ctx context.Context, directory string, extraEnv []string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), extraEnv...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
