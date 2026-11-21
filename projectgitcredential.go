package main

import (
	"context"
	"errors"
	"strings"
	"time"

	"ideploy/internal/gitauth"
	"ideploy/internal/project"
)

func (s *SystemService) GetProjectGitSummary(id string) (project.GitSummary, error) {
	store, err := s.projectStore()
	if err != nil {
		return project.GitSummary{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	value, err := store.Get(ctx, id)
	if err != nil {
		return project.GitSummary{}, err
	}
	summary, err := project.ResolveGitSummarySource(ctx, value)
	if err != nil {
		summary.Detail = err.Error()
		return summary, nil
	}
	if value.SourceKind == project.SourceLocal && summary.ReadLocalSync(ctx, value.LocalPath) {
		return summary, nil
	}
	var env []string
	if value.SourceKind == project.SourceGit && value.CredentialRef != "" {
		credential, err := gitauth.Load(ctx, value.CredentialRef)
		if err != nil {
			summary.Detail = err.Error()
			return summary, nil
		}
		var cleanup func()
		env, cleanup, err = gitauth.Environment(credential)
		if err != nil {
			return summary, err
		}
		defer cleanup()
	}
	if err := summary.Load(ctx, env); err != nil {

		summary.Detail = err.Error()
		if ctx.Err() != nil {
			summary.Detail = "读取远端 Git 信息超时"
		}
	}
	return summary, nil
}

func (s *SystemService) saveProjectGitCredential(ctx context.Context, projects *project.Store, id string, input ProjectInput) error {
	if err := validateProjectGitCredentialInput(input); err != nil {
		return err
	}
	user := strings.TrimSpace(input.GitUsername)
	ref := gitauth.Ref(id)
	if err := gitauth.Save(ctx, ref, gitauth.Credential{Username: user, Token: input.GitToken}); err != nil {
		return err
	}
	if err := projects.SetCredentialRef(ctx, id, ref); err != nil {
		_ = gitauth.Delete(ctx, ref)
		return err
	}
	return nil
}

func validateProjectGitCredentialInput(input ProjectInput) error {
	if input.GitToken == "" {
		return nil
	}
	if input.SourceKind != "git" || !strings.HasPrefix(input.RepositoryURL, "https://") {
		return errors.New("Git 令牌仅支持 HTTPS 仓库")
	}
	if strings.TrimSpace(input.GitUsername) == "" || strings.ContainsAny(input.GitUsername, "\r\n") {
		return errors.New("Git 用户名不能为空且不能包含换行")
	}
	return nil
}
