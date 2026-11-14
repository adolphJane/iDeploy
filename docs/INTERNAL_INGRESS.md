# 内部应用入口

在“设置 → 应用入口”选择完整域名、入口服务器和本机端口。可从已有域名列表选择，也可填写其他域名。先查找或申请 HTTPS 证书，再保存参数、运行预检并部署入口。预检会检查 DNS、现有 Nginx、证书覆盖范围和证书挂载。入口服务仅绑定服务器的 `127.0.0.1`，由现有 Nginx 提供公网 HTTPS。

入口状态为“已就绪”后，可在部署的“发布新版本”页面选择该服务器上的入口域名、单级路径和应用容器 HTTP 端口。例如选择 `apps.example.com` 与 `demo`，发布成功后访问 `https://apps.example.com/demo/`。同一容器端口匹配多个后端时填写容器名称或 Compose 服务名。

## 项目路径前缀

iDeploy 根据本次发布选择的路径传入 `IDEPLOY_BASE_PATH`，值包含首尾斜线，例如 `/demo/`。单容器 Dockerfile 构建会收到同名 `--build-arg`，运行容器会收到同名环境变量。Dockerfile 需要声明 `ARG IDEPLOY_BASE_PATH`，并在构建前端资源时使用它。应用运行时也要用环境变量配置 API、静态文件和服务端路由；前端路由、分享链接同样应使用该前缀。iDeploy 保留请求路径，不会改写页面或 JavaScript 中的根路径链接。

例如 Vite 项目可将 `base` 设为 `process.env.IDEPLOY_BASE_PATH || '/'`，在构建阶段的 Dockerfile 中声明 `ARG IDEPLOY_BASE_PATH=/` 与 `ENV IDEPLOY_BASE_PATH=${IDEPLOY_BASE_PATH}`。Vue Router 可使用 `createWebHistory(import.meta.env.BASE_URL)`。运行服务读取 `process.env.IDEPLOY_BASE_PATH || '/'`。更换发布路径后需重新构建发布，旧镜像的前端资源路径不会自动变化。

Compose 发布也会在本机解析与构建时提供 `IDEPLOY_BASE_PATH`。Compose 文件须显式把 `${IDEPLOY_BASE_PATH}` 传给需要它的服务的 `build.args` 和 `environment`；解析后的 Compose 快照会随发布制品保存，供失败重试和回滚使用。

新发布在构建与上传前检查入口状态，并将路径选择写入发布记录。发布任务在新工作负载通过健康检查后应用 Nginx 路由，再标记发布成功。失败重试保留原路径选择。路径由部署独占；更换已生效的域名或路径时，先在部署页面解除旧路由，再选择新入口发布。可以在同一服务器配置多个入口域名，但每个域名使用不同的本机端口。
