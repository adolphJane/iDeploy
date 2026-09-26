package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AIProjectSummary returns a bounded, redacted-by-default project snapshot for
// an external model. Only common manifests and shallow source metadata are
// included; dependency directories and secrets are never traversed.
func AIProjectSummary(root, contextPath string) (string, error) {
	base, err := projectContextRoot(root, contextPath)
	if err != nil {
		return "", err
	}
	candidates := []string{"package.json", "pnpm-lock.yaml", "yarn.lock", "go.mod", "go.sum", "requirements.txt", "pyproject.toml", "pom.xml", "build.gradle", "build.gradle.kts", "Cargo.toml", "README.md"}
	var builder strings.Builder
	for _, name := range candidates {
		contents, err := os.ReadFile(filepath.Join(base, name))
		if err != nil {
			continue
		}
		if len(contents) > 12000 {
			contents = contents[:12000]
		}
		builder.WriteString(fmt.Sprintf("FILE %s\n---\n%s\n---\n", name, sanitizeForPrompt(string(contents))))
	}
	if builder.Len() == 0 {
		return "项目上下文中未找到可识别的常见清单文件。", nil
	}
	return builder.String(), nil
}

func sanitizeForPrompt(value string) string {
	lines := strings.Split(value, "\n")
	for index, line := range lines {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "api_key") || strings.Contains(lower, "private_key") {
			lines[index] = "[REDACTED]"
		}
	}
	return strings.Join(lines, "\n")
}
