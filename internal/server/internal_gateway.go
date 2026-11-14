package server

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ValidateGatewayOwnership prevents the internal ingress job from replacing an unrelated container.
func (r *Remote) ValidateGatewayOwnership(ctx context.Context, connection Connection, name, fqdn string) error {
	if !validContainerID(name) || !validIngressFQDN(fqdn) {
		return errors.New("内部入口容器标识无效")
	}
	output, err := r.runTimeout(ctx, connection, `docker ps -a --format '{{.Names}}'`, 15*time.Second)
	if err != nil {
		return err
	}
	found := false
	for _, candidate := range strings.Fields(output) {
		if candidate == name {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	labels, err := r.runTimeout(ctx, connection, "docker inspect --format '{{index .Config.Labels \"com.ideploy.role\"}}|{{index .Config.Labels \"com.ideploy.fqdn\"}}' "+shellQuote(name), 15*time.Second)
	if err != nil {
		return err
	}
	if strings.TrimSpace(labels) != "apps-gateway|"+fqdn {
		return errors.New("同名容器不属于此 iDeploy 应用入口，停止替换")
	}
	return nil
}
