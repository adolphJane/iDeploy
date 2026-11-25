package deployment

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"ideploy/internal/platform/environment"
)

func preserveDeploymentAssociations(input *Input, current Target) {
	if input.Environment == "" {
		input.Environment = current.Environment
	}
	if input.DatabaseConnectionID == nil {
		input.DatabaseConnectionID = &current.DatabaseConnectionID
	}
}

func databaseConnectionID(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func deploymentConfigurationError(err error) error {
	switch {
	case strings.Contains(err.Error(), "deployment_targets.project_id, deployment_targets.server_id"):
		return errors.New("同一项目的部署目标必须选择不同服务器")
	case strings.Contains(err.Error(), "deployment_targets.project_id, deployment_targets.environment"):
		return errors.New("此项目已配置该环境")
	default:
		return fmt.Errorf("保存部署配置: %w", err)
	}
}

var scopedImageTag = regexp.MustCompile(`^ideploy-env-[a-f0-9]{32}-`)

func releaseImageTag(ctx context.Context, tx *sql.Tx, input ReleaseInput, targetEnvironment string) (string, error) {
	sourceID := input.RollbackToID
	if sourceID == "" && input.AllowRetry {
		sourceID = input.PolicySourceID
	}
	if sourceID != "" {
		var tag string
		err := tx.QueryRowContext(ctx, "SELECT image_tag FROM deployment_releases WHERE id=? AND deployment_id=?", sourceID, input.DeploymentID).Scan(&tag)
		return tag, err
	}
	if targetEnvironment == environment.Default {
		if scopedImageTag.MatchString(input.Version) {
			return "", errors.New("版本号使用了部署环境保留的镜像标签前缀")
		}
		return "", nil
	}
	prefix := "ideploy-env-" + input.DeploymentID + "-"
	version := input.Version
	if len(prefix)+len(version) > 128 {
		hash := sha256.Sum256([]byte(version))
		version = version[:128-len(prefix)-1-32] + "-" + fmt.Sprintf("%x", hash[:16])
	}
	return prefix + version, nil
}

func (release Release) ArtifactTag() string {
	if release.ImageTag != "" {
		return release.ImageTag
	}
	return release.Version
}
