package project

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
)

type GitSummary struct {
	RepositoryURL  string `json:"repositoryUrl"`
	Ref            string `json:"ref"`
	UpdatedAt      string `json:"updatedAt"`
	SyncKnown      bool   `json:"syncKnown"`
	Ahead          int    `json:"ahead"`
	Behind         int    `json:"behind"`
	WorkingChanges int    `json:"workingChanges"`
	Detail         string `json:"detail"`
}

func ResolveGitSummarySource(ctx context.Context, value Project) (GitSummary, error) {
	result := GitSummary{RepositoryURL: value.RepositoryURL, Ref: value.RefValue}
	if value.SourceKind == SourceLocal {
		root, err := gitOutput(ctx, value.LocalPath, "rev-parse", "--show-toplevel")
		if err != nil {
			return result, errors.New("项目目录不是 Git 工作区，或 Git 不可用")
		}
		branch, _ := gitOutput(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
		remote := "origin"
		result.Ref = "HEAD"
		if branch != "" {
			if name, err := gitOutput(ctx, root, "config", "--get", "branch."+branch+".remote"); err == nil && name != "." && name != "" {
				remote = name
				if ref, err := gitOutput(ctx, root, "config", "--get", "branch."+branch+".merge"); err == nil && strings.HasPrefix(ref, "refs/heads/") {
					result.Ref = ref
				}
			}
		}
		address, err := gitOutput(ctx, root, "remote", "get-url", "--", remote)
		if err != nil || !validGitURL(address) {
			return result, errors.New("本地 Git 未配置可用的 HTTPS 或 SSH 远端")
		}
		result.RepositoryURL = address
	} else {
		switch value.RefKind {
		case RefBranch:
			result.Ref = "refs/heads/" + strings.TrimPrefix(value.RefValue, "refs/heads/")
		case RefTag:
			result.Ref = "refs/tags/" + strings.TrimPrefix(value.RefValue, "refs/tags/")
		}
	}
	if !validGitURL(result.RepositoryURL) {
		result.RepositoryURL = ""
		return result, errors.New("Git 远端地址无效")
	}
	return result, nil
}

func (summary *GitSummary) Load(ctx context.Context, env []string) error {
	dir, err := os.MkdirTemp("", "ideploy-git-summary-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	if env == nil {
		env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "SSH_ASKPASS=/usr/bin/false")
	}
	command := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "protocol.allow=never", "-c", "protocol.https.allow=always", "-c", "protocol.ssh.allow=always"}, args...)...)
		cmd.Env = env
		return cmd
	}
	if err := command("init", "--bare", "--quiet").Run(); err != nil {
		return errors.New("无法创建临时 Git 工作区，请检查本机 Git")
	}
	if err := command("fetch", "--quiet", "--no-tags", "--depth=1", "--filter=tree:0", "--", summary.RepositoryURL, summary.Ref).Run(); err != nil {
		return errors.New("读取远端 Git 失败，请检查网络、仓库凭据和引用")
	}
	if summary.Ref == "HEAD" {
		if output, err := command("ls-remote", "--symref", "--", summary.RepositoryURL, "HEAD").Output(); err == nil {
			for _, line := range strings.Split(string(output), "\n") {
				left, right, ok := strings.Cut(line, "\t")
				if ok && right == "HEAD" && strings.HasPrefix(left, "ref: refs/heads/") {
					summary.Ref = strings.TrimPrefix(left, "ref: ")
					break
				}
			}
		}
	}
	updated, err := command("show", "-s", "--format=%cI", "FETCH_HEAD").Output()
	if err != nil {
		return errors.New("读取远端最新提交时间失败")
	}
	summary.UpdatedAt = strings.TrimSpace(string(updated))
	return nil
}

func (summary *GitSummary) ReadLocalSync(ctx context.Context, directory string) bool {
	status := InspectGit(ctx, directory, summary.RepositoryURL != "" && summary.Ref != "HEAD")
	summary.WorkingChanges = status.ChangedFiles + status.Untracked
	if !status.RemoteFresh {
		return false
	}
	updated, err := gitOutput(ctx, directory, "show", "-s", "--format=%cI", "@{upstream}")
	if err != nil || !status.DivergenceKnown {
		return false
	}
	summary.UpdatedAt = updated
	summary.Ahead, summary.Behind = status.Ahead, status.Behind
	summary.SyncKnown = true
	return true
}
