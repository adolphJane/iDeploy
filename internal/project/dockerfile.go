package project

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type DockerfileSuggestion struct {
	Content    string
	Technology string
	Source     string
	Warnings   []string
}

var exposeInstructionPattern = regexp.MustCompile(`(?i)^\s*EXPOSE\s+(.+)$`)
var exposedPortPattern = regexp.MustCompile(`^(\d{1,5})(?:/(tcp|udp))?$`)
var externalPortPattern = regexp.MustCompile(`^(\d{1,5})(?:/(tcp|udp))?$`)

// ResolveContainerPorts expands bare host ports such as "8080" using the
// Dockerfile EXPOSE declarations. Complete mappings remain untouched.
func ResolveContainerPorts(root, contextPath, dockerfilePath string, configured []string) ([]string, error) {
	resolved := make([]string, len(configured))
	copy(resolved, configured)
	bare := make([]int, 0)
	for index, value := range resolved {
		value = strings.TrimSpace(value)
		if strings.Contains(value, ":") {
			resolved[index] = value
			continue
		}
		if !externalPortPattern.MatchString(value) {
			return nil, fmt.Errorf("端口映射 %q 格式无效", value)
		}
		bare = append(bare, index)
	}
	if len(bare) == 0 {
		return resolved, nil
	}
	if strings.TrimSpace(dockerfilePath) == "" {
		return nil, errors.New("未配置 Dockerfile，无法自动匹配内部端口")
	}
	if _, err := projectContextRoot(root, contextPath); err != nil {
		return nil, err
	}
	projectRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	dockerfileFile := filepath.Clean(filepath.Join(projectRoot, filepath.FromSlash(dockerfilePath)))
	if !isWithin(filepath.Clean(projectRoot), dockerfileFile) {
		return nil, errors.New("Dockerfile path escapes project path")
	}
	contents, err := os.ReadFile(dockerfileFile)
	if err != nil {
		return nil, fmt.Errorf("读取 Dockerfile 以匹配内部端口: %w", err)
	}
	exposed := parseExposedPorts(string(contents))
	if len(exposed) == 0 {
		return nil, errors.New("Dockerfile 未声明 EXPOSE，无法自动匹配内部端口；请填写完整的 外部端口:内部端口")
	}
	if len(bare) == 1 {
		externalPort, _ := splitPortProtocol(resolved[bare[0]])
		for _, candidate := range exposed {
			internalPort, _ := splitPortProtocol(candidate)
			if externalPort == internalPort {
				resolved[bare[0]] = barePortMapping(resolved[bare[0]], candidate)
				return resolved, nil
			}
		}
	}
	if len(bare) == 1 && len(exposed) == 1 {
		resolved[bare[0]] = barePortMapping(resolved[bare[0]], exposed[0])
		return resolved, nil
	}
	if len(bare) != len(exposed) {
		return nil, fmt.Errorf("检测到 %d 个外部端口和 %d 个 EXPOSE 端口，无法唯一匹配；请填写完整的 外部端口:内部端口", len(bare), len(exposed))
	}
	for index, configuredIndex := range bare {
		resolved[configuredIndex] = barePortMapping(resolved[configuredIndex], exposed[index])
	}
	return resolved, nil
}

func parseExposedPorts(contents string) []string {
	ports := make([]string, 0)
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		matches := exposeInstructionPattern.FindStringSubmatch(line)
		if len(matches) != 2 {
			continue
		}
		for _, value := range strings.Fields(matches[1]) {
			if exposedPortPattern.MatchString(value) {
				ports = append(ports, value)
			}
		}
	}
	return ports
}

func barePortMapping(external, internal string) string {
	externalPort, protocol := splitPortProtocol(external)
	internalPort, internalProtocol := splitPortProtocol(internal)
	if protocol == "" {
		protocol = internalProtocol
	}
	if protocol == "" {
		return externalPort + ":" + internalPort
	}
	return externalPort + ":" + internalPort + "/" + protocol
}

func splitPortProtocol(value string) (string, string) {
	parts := strings.SplitN(value, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return value, ""
}

// GenerateDockerfile uses deterministic templates for common stacks. It is
// deliberately conservative: uncertain commands are marked as TODO instead
// of silently guessing a production entrypoint.
func GenerateDockerfile(root, contextPath string) (DockerfileSuggestion, error) {
	base, err := projectContextRoot(root, contextPath)
	if err != nil {
		return DockerfileSuggestion{}, err
	}
	has := func(name string) bool { _, err := os.Stat(filepath.Join(base, name)); return err == nil }
	if has("go.mod") {
		moduleFiles := "COPY go.mod ./"
		if has("go.sum") {
			moduleFiles = "COPY go.mod go.sum ./"
		}
		return DockerfileSuggestion{
			Technology: "Go",
			Source:     "go.mod",
			Content: fmt.Sprintf(`FROM golang:1.25-alpine AS build
WORKDIR /src
%s
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/app .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app"]
`, moduleFiles), Warnings: []string{"请确认 Go 项目的入口包位于构建上下文根目录；默认端口为 8080。"}}, nil
	}
	if has("package.json") {
		content, _ := os.ReadFile(filepath.Join(base, "package.json"))
		var manifest struct {
			PackageManager string            `json:"packageManager"`
			Scripts        map[string]string `json:"scripts"`
		}
		_ = json.Unmarshal(content, &manifest)
		install := "npm ci --omit=dev"
		copy := "COPY package*.json ./"
		if !has("package-lock.json") {
			install = "npm install --omit=dev"
		}
		if has("pnpm-lock.yaml") || strings.HasPrefix(manifest.PackageManager, "pnpm@") {
			install = "corepack enable && pnpm install --frozen-lockfile --prod"
			copy = "COPY package.json pnpm-lock.yaml ./"
		} else if has("yarn.lock") || strings.HasPrefix(manifest.PackageManager, "yarn@") {
			install = "corepack enable && yarn install --frozen-lockfile --production"
			copy = "COPY package.json yarn.lock ./"
		}
		startWarning := "请确认 npm start 是正确的生产启动命令和端口（默认 3000）。"
		if _, ok := manifest.Scripts["start"]; !ok {
			startWarning = "项目没有明确的 start script；请把 CMD 改为实际生产启动命令。"
		}
		return DockerfileSuggestion{
			Technology: "Node.js",
			Source:     "package.json",
			Content: fmt.Sprintf(`FROM node:22-alpine
WORKDIR /app
%s
RUN %s
COPY . .
ENV NODE_ENV=production
EXPOSE 3000
CMD ["npm", "start"]
`, copy, install), Warnings: []string{startWarning}}, nil
	}
	if has("requirements.txt") || has("pyproject.toml") {
		install := "COPY requirements.txt ./\nRUN pip install --no-cache-dir -r requirements.txt"
		source := "requirements.txt"
		if !has("requirements.txt") {
			install = "COPY pyproject.toml ./\nRUN pip install --no-cache-dir ."
			source = "pyproject.toml"
		}
		return DockerfileSuggestion{
			Technology: "Python",
			Source:     source,
			Content: fmt.Sprintf(`FROM python:3.12-slim
WORKDIR /app
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
%s
COPY . .
EXPOSE 8000
CMD ["python", "app.py"]
`, install), Warnings: []string{"默认使用 app.py 和 8000 端口；请确认实际入口文件、启动参数和监听端口。"}}, nil
	}
	if has("pom.xml") || has("build.gradle") || has("build.gradle.kts") {
		buildFile := "pom.xml"
		buildCommand := "mvn -q -DskipTests package"
		artifactPath := "/src/target/*.jar"
		if !has("pom.xml") {
			buildFile = "build.gradle"
			if has("build.gradle.kts") {
				buildFile = "build.gradle.kts"
			}
			buildCommand = "./gradlew bootJar -x test"
			artifactPath = "/src/build/libs/*.jar"
		}
		return DockerfileSuggestion{
			Technology: "Java",
			Source:     buildFile,
			Content: fmt.Sprintf(`FROM eclipse-temurin:21-jdk AS build
WORKDIR /src
COPY . .
RUN %s

FROM eclipse-temurin:21-jre
WORKDIR /app
COPY --from=build %s /app/app.jar
EXPOSE 8080
ENTRYPOINT ["java", "-jar", "/app/app.jar"]
`, buildCommand, artifactPath), Warnings: []string{"默认使用 Java 21、8080 端口；请确认 Maven/Gradle 产物路径。"}}, nil
	}
	return DockerfileSuggestion{Warnings: []string{"未识别到常见技术栈，建议使用 DeepSeek 生成或手动编写 Dockerfile。"}}, nil
}

func WriteGeneratedDockerfile(root, contextPath, content string, overwrite bool) (string, error) {
	base, err := projectContextRoot(root, contextPath)
	if err != nil {
		return "", err
	}
	content = strings.TrimSpace(content)
	if content == "" || !strings.Contains(strings.ToUpper(content), "FROM ") {
		return "", errors.New("Dockerfile 内容无效：必须包含 FROM 指令")
	}
	target := filepath.Join(base, "Dockerfile")
	if _, err := os.Stat(target); err == nil {
		if !overwrite {
			return "", errors.New("目标 Dockerfile 已存在，请确认覆盖后再写入")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.WriteFile(target, []byte(content+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("写入 Dockerfile: %w", err)
	}
	if contextPath == "" || contextPath == "." {
		return "Dockerfile", nil
	}
	return filepath.ToSlash(filepath.Join(contextPath, "Dockerfile")), nil
}

func projectContextRoot(root, contextPath string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return "", errors.New("local project path is empty")
	}
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	base = filepath.Clean(base)
	if contextPath != "" && contextPath != "." {
		candidate := filepath.Clean(filepath.Join(base, filepath.FromSlash(contextPath)))
		if !isWithin(base, candidate) {
			return "", errors.New("build context escapes project path")
		}
		base = candidate
	}
	info, err := os.Stat(base)
	if err != nil || !info.IsDir() {
		return "", errors.New("构建上下文目录不可用")
	}
	return base, nil
}
