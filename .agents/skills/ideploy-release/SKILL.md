---
name: ideploy-release
description: Use iDeploy MCP to publish, verify, or roll back a workload when the user explicitly chooses iDeploy. Includes release preflight, image and storage verification, and reconnect recovery.
---

# Release with iDeploy

1. Use the iDeploy MCP tools to identify the deployment, registry, ready HTTPS ingress, and requested version. Read the deployment policy and live runtime to check the observed image, mounts, and registered data directories.
2. Run `preflight_deployment` with the intended ingress, path, service, and port. Stop on a failed check and report its details. Use the returned policy revision in `publish_deployment`.
3. Submit a publish only for the requested target. Supply a stable `request_id` and reuse it for the same request after a timeout. If the receipt is pending, check `get_operation`. Use the existing job ID with `wait_job` or `get_job`.
4. After completion, use `list_releases`, `get_release_runtime_verification`, and `inspect_deployment_runtime` to confirm the actual image Digest and storage checks. Report when iDeploy saved the artifact but requires reconciliation.
5. For an explicitly requested rollback, select the intended successful release and call `rollback_deployment`. Verify the resulting runtime. For an explicitly requested retry, use `retry_release` to reuse a saved artifact.
6. Route iDeploy work through its MCP tools. If they are unavailable, report the connection problem. Keep policy changes separate from release execution and make them only when requested.
