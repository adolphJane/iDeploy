package registry

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type ProbeResult struct {
	Status Status
	Detail string
}

type Tester struct {
	client *http.Client
}

func NewTester() *Tester {
	return &Tester{client: &http.Client{Timeout: 8 * time.Second}}
}

func (t *Tester) Probe(ctx context.Context, connection Connection) ProbeResult {
	path := "/v2/"
	if connection.Kind == KindHarbor {
		path = "/api/v2.0/health"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(connection.Endpoint, "/")+path, nil)
	if err != nil {
		return ProbeResult{Status: StatusDegraded, Detail: fmt.Sprintf("无法创建检测请求：%v", err)}
	}
	password := connection.Password
	if password == "" && connection.CredentialRef != "" {
		password, _ = LoadPassword(ctx, connection.CredentialRef)
	}
	if connection.Username != "" && password != "" {
		request.SetBasicAuth(connection.Username, password)
	}

	response, err := t.client.Do(request)
	if err != nil {
		return ProbeResult{Status: StatusUnreachable, Detail: fmt.Sprintf("连接失败：%v", err)}
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		if connection.Kind == KindHarbor {
			return ProbeResult{Status: StatusHealthy, Detail: "Harbor API 可用"}
		}
		return ProbeResult{Status: StatusHealthy, Detail: "OCI Registry 可用"}
	}
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ProbeResult{Status: StatusAuthRequired, Detail: "仓库可达，需要配置凭据"}
	}
	return ProbeResult{Status: StatusDegraded, Detail: fmt.Sprintf("服务返回 HTTP %d", response.StatusCode)}
}
