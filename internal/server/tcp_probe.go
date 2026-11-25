package server

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

func (r *Remote) ProbeTCP(ctx context.Context, connection Connection, host string, port int) error {
	_, err := r.runTimeout(ctx, connection, "command -v nc >/dev/null 2>&1 || { printf %s IDEPLOY_NC_MISSING >&2; exit 1; }; nc -z -w 4 "+shellQuote(host)+" "+strconv.Itoa(port), 12*time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "IDEPLOY_NC_MISSING") {
			return errors.New("目标服务器需要安装 nc 才能测试数据库端口")
		}
		return errors.New("数据库 TCP 连接未通过")
	}
	return nil
}
