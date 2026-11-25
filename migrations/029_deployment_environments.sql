ALTER TABLE deployment_targets ADD COLUMN environment TEXT NOT NULL DEFAULT 'default'
    CHECK (environment IN ('default', 'test', 'production'));

CREATE UNIQUE INDEX idx_deployment_project_environment
    ON deployment_targets(project_id, environment) WHERE environment <> 'default';

ALTER TABLE database_connections ADD COLUMN environment TEXT NOT NULL DEFAULT 'default'
    CHECK (environment IN ('default', 'test', 'production'));

ALTER TABLE deployment_targets ADD COLUMN database_connection_id TEXT
    REFERENCES database_connections(id) ON DELETE RESTRICT;

CREATE INDEX idx_deployment_database_connection
    ON deployment_targets(database_connection_id) WHERE database_connection_id IS NOT NULL;

CREATE TRIGGER validate_deployment_database_insert
BEFORE INSERT ON deployment_targets
WHEN NEW.database_connection_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM database_connections
    WHERE id = NEW.database_connection_id AND environment = NEW.environment
)
BEGIN
    SELECT RAISE(ABORT, '数据库必须与部署目标属于同一环境');
END;

CREATE TRIGGER validate_deployment_database_update
BEFORE UPDATE OF environment, database_connection_id ON deployment_targets
WHEN NEW.database_connection_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM database_connections
    WHERE id = NEW.database_connection_id AND environment = NEW.environment
)
BEGIN
    SELECT RAISE(ABORT, '数据库必须与部署目标属于同一环境');
END;

CREATE TRIGGER prevent_bound_database_environment_change
BEFORE UPDATE OF environment ON database_connections
WHEN NEW.environment <> OLD.environment AND EXISTS (
    SELECT 1 FROM deployment_targets WHERE database_connection_id = OLD.id
)
BEGIN
    SELECT RAISE(ABORT, '请先解除应用部署的数据库绑定，再修改数据库环境');
END;

ALTER TABLE deployment_releases ADD COLUMN image_tag TEXT NOT NULL DEFAULT '';

CREATE TRIGGER track_deployment_database_binding
AFTER UPDATE OF database_connection_id ON deployment_targets
WHEN NEW.workload_kind = 'container' AND NEW.database_connection_id IS NOT OLD.database_connection_id     AND NEW.environment_revision = OLD.environment_revision
BEGIN
    UPDATE deployment_targets SET environment_revision = environment_revision + 1 WHERE id = NEW.id;
END;

CREATE TRIGGER track_deployment_database_endpoint
AFTER UPDATE OF host, port, database_name ON database_connections
WHEN NEW.host <> OLD.host OR NEW.port <> OLD.port OR NEW.database_name <> OLD.database_name
BEGIN
    UPDATE deployment_targets SET environment_revision = environment_revision + 1
    WHERE database_connection_id = NEW.id AND workload_kind = 'container';
END;

CREATE TRIGGER validate_deployment_environment_server_insert
BEFORE INSERT ON deployment_targets
WHEN NEW.environment <> 'default' AND EXISTS (
    SELECT 1 FROM deployment_targets d
    JOIN servers other ON other.id = d.server_id
    JOIN servers target ON target.id = NEW.server_id
    WHERE d.project_id = NEW.project_id AND d.environment <> 'default' AND d.environment <> NEW.environment
      AND d.server_id <> NEW.server_id AND lower(rtrim(other.host,'.')) = lower(rtrim(target.host,'.')) AND other.port = target.port
)
BEGIN
    SELECT RAISE(ABORT, '测试与线上环境必须选择不同服务器');
END;

CREATE TRIGGER validate_deployment_environment_server_update
BEFORE UPDATE OF project_id, server_id, environment ON deployment_targets
WHEN NEW.environment <> 'default' AND EXISTS (
    SELECT 1 FROM deployment_targets d
    JOIN servers other ON other.id = d.server_id
    JOIN servers target ON target.id = NEW.server_id
    WHERE d.id <> NEW.id AND d.project_id = NEW.project_id AND d.environment <> 'default' AND d.environment <> NEW.environment
      AND d.server_id <> NEW.server_id AND lower(rtrim(other.host,'.')) = lower(rtrim(target.host,'.')) AND other.port = target.port
)
BEGIN
    SELECT RAISE(ABORT, '测试与线上环境必须选择不同服务器');
END;
