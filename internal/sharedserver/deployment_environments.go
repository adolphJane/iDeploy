package sharedserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"ideploy/internal/platform/environment"
)

func hasDeploymentEnvironmentConstraints(kind string) bool {
	return kind == "r.deployments" || kind == "r.databases" || kind == "r.servers"
}

func lockWorkspaceMutation(ctx context.Context, tx *sql.Tx) error {
	var id int
	return tx.QueryRowContext(ctx, "SELECT id FROM shared_workspace WHERE id=1 FOR UPDATE").Scan(&id)
}

type environmentResource struct {
	Environment          string `json:"environment"`
	ProjectID            string `json:"project_id"`
	ServerID             string `json:"server_id"`
	DatabaseConnectionID string `json:"database_connection_id"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
}

func validateDeploymentEnvironments(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT kind,id,data FROM shared_resources WHERE deleted=false AND kind IN ('r.deployments','r.databases','r.servers')")
	if err != nil {
		return err
	}
	defer rows.Close()
	databases := map[string]string{}
	servers := map[string]string{}
	deployments := []environmentResource{}
	for rows.Next() {
		var kind, id string
		var data []byte
		if err := rows.Scan(&kind, &id, &data); err != nil {
			return err
		}
		var item environmentResource
		if err := json.Unmarshal(data, &item); err != nil {
			return err
		}
		value, err := environment.Normalize(item.Environment)
		if err != nil || item.Environment != "" && value != item.Environment {
			return errors.New("invalid deployment environment")
		}
		item.Environment = value
		switch kind {
		case "r.databases":
			databases[id] = item.Environment
		case "r.servers":
			servers[id] = strings.ToLower(strings.TrimRight(item.Host, ".")) + ":" + strconv.Itoa(item.Port)
		case "r.deployments":
			deployments = append(deployments, item)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	namedTargets := map[[2]string]bool{}
	namedServers := map[[2]string]bool{}
	for _, target := range deployments {
		if target.DatabaseConnectionID != "" && databases[target.DatabaseConnectionID] != target.Environment {
			return errors.New("database connection must belong to the deployment environment")
		}
		if target.Environment == environment.Default {
			continue
		}
		group := [2]string{target.ProjectID, target.Environment}
		if namedTargets[group] {
			return errors.New("project already has a deployment in this environment")
		}
		namedTargets[group] = true
		endpoint, ok := servers[target.ServerID]
		if !ok {
			return errors.New("deployment environment is missing its target server")
		}
		host := [2]string{target.ProjectID, endpoint}
		if namedServers[host] {
			return errors.New("test and production deployments must use different servers")
		}
		namedServers[host] = true
	}
	return nil
}
