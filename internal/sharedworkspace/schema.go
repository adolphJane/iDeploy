package sharedworkspace

// EntitySpec is the portable portion of a local SQLite table. Device paths,
// Keychain references, runtime status and potentially secret text are omitted.
type EntitySpec struct {
	Kind    string
	Table   string
	ID      string
	Columns []string
}

var Entities = []EntitySpec{
	{"r.projects", "projects", "id", []string{"id", "name", "description", "created_at", "updated_at", "deleted_at"}},
	{"r.project_sources", "project_sources", "project_id", []string{"project_id", "kind", "repository_url", "ref_kind", "ref_value", "resolved_commit"}},
	{"r.build_configs", "build_configs", "project_id", []string{"project_id", "context_path", "dockerfile_path", "compose_path", "image_repository", "target", "platforms_json"}},
	{"r.servers", "servers", "id", []string{"id", "name", "host", "port", "ssh_user", "auth_method", "created_at"}},
	{"r.registries", "registry_connections", "id", []string{"id", "name", "kind", "endpoint", "username", "created_at"}},
	{"r.databases", "database_connections", "id", []string{"id", "name", "driver", "host", "port", "database_name", "username", "ssl_mode", "created_at", "environment"}},
	{"r.deployments", "deployment_targets", "id", []string{"id", "project_id", "server_id", "desired_state", "container_ports_json", "restart_policy", "workload_kind", "workload_name", "build_mode", "environment_revision", "created_at", "updated_at", "environment", "database_connection_id"}},
	{"r.runtime_policies", "deployment_runtime_policies", "deployment_id", []string{"deployment_id", "revision", "policy_json"}},
	{"r.publish_paths", "deployment_publish_paths", "deployment_id", []string{"deployment_id", "fqdn", "path_prefix", "service_name", "container_port", "updated_at"}},
	{"r.env", "deployment_environment_variables", "id", []string{"id", "deployment_id", "name", "kind", "build_secret", "created_at", "updated_at"}},
	{"r.releases", "deployment_releases", "id", []string{"id", "deployment_id", "registry_id", "action", "version", "image_ref", "source_revision", "rollback_to_id", "status", "created_at", "finished_at", "environment_revision", "build_mode", "source_commit", "delivery_ref", "delivery_commit", "delivery_digest", "remote_job_id", "image_tag"}},
	{"r.release_policies", "release_runtime_policies", "release_id", []string{"release_id", "revision", "policy_json", "before_json", "after_json"}},
	{"r.release_path_intents", "release_path_intents", "release_id", []string{"release_id", "fqdn", "path_prefix", "service_name", "container_port"}},
	{"r.dns_connections", "dns_connections", "id", []string{"id", "name", "provider", "created_at", "updated_at"}},
	{"r.domains", "domains", "id", []string{"id", "name", "note", "dns_connection_id", "created_at", "updated_at"}},
	{"r.domain_hosts", "domain_hosts", "id", []string{"id", "domain_id", "label", "fqdn", "note", "created_at", "updated_at"}},
	{"r.domain_bindings", "domain_bindings", "id", []string{"id", "host_id", "deployment_id", "service_name", "container_port", "upstream_protocol", "host_ip", "host_port", "certificate_path", "certificate_key_path", "ingress_mode", "status", "created_at", "updated_at"}},
	{"r.internal_ingresses", "internal_ingresses", "fqdn", []string{"fqdn", "server_id", "gateway_port", "certificate_path", "certificate_key_path", "webroot", "status", "job_id", "updated_at"}},
	{"r.ingresses", "server_ingresses", "server_id", []string{"server_id", "config_hash", "image_ref", "updated_at"}},
	{"r.external_routes", "nginx_external_routes", "host_id", []string{"host_id", "server_id", "host_ip", "host_port", "upstream_scheme", "certificate_path", "certificate_key_path", "status", "updated_at"}},
	{"r.path_routes", "shared_path_routes", "id", []string{"id", "fqdn", "path_prefix", "deployment_id", "service_name", "container_port", "host_ip", "host_port", "certificate_path", "certificate_key_path", "status", "created_at", "updated_at"}},
	{"r.job_summaries", "shared_job_summaries", "id", []string{"id", "type", "lock_key", "status", "progress", "created_at", "updated_at"}},
}

func Entity(kind string) (EntitySpec, bool) {
	for _, value := range Entities {
		if value.Kind == kind {
			return value, true
		}
	}
	return EntitySpec{}, false
}

func AllowedField(kind, field string) bool {
	spec, ok := Entity(kind)
	if !ok {
		return false
	}
	for _, column := range spec.Columns {
		if column == field {
			return true
		}
	}
	return false
}
