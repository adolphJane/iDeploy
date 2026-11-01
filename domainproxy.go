package main

import (
	"context"
	"ideploy/internal/server"
	"time"
)

func (s *SystemService) ScanServerNginxRoutes(serverID string) (server.NginxScan, error) {
	servers, err := s.serverStore()
	if err != nil {
		return server.NginxScan{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	connection, err := servers.Get(ctx, serverID)
	if err != nil {
		return server.NginxScan{}, err
	}
	return server.NewRemote().ScanExistingNginx(ctx, connection)
}
