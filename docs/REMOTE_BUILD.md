# iDeploy 远端构建

远端构建适用于本机没有可用 Docker 的情况。部署目标选择“iDeploy 服务构建”后，发布会从 Git 固定提交生成非秘密部署计划并推送到 Git；共享服务的构建执行器在服务器指定工作目录检出该提交，构建并推送镜像。目标服务器随后拉取按 Digest 锁定的制品并部署。失败发布可从服务端任务恢复已推送制品，历史制品可用于回滚。

## 使用条件

- 项目来源为 Git，仓库地址不含内嵌密码或 Token。私有 HTTPS 仓库凭据保存在本机 Keychain，并经加密共享工作区按任务租约提供给构建执行器。
- iDeploy 共享服务与构建执行器已就绪；设置页会显示服务端 Docker 状态。构建服务器需要 Docker、Buildx 和 Compose 插件。目标服务器仍需 Docker。
- 发布仓库提供可用的推送凭据。远端构建使用服务端 Docker 的默认网络。
- 单容器项目配置 Dockerfile 与目标镜像仓库；Compose 项目配置 Compose 文件，所有需要构建的服务都要有属于所选 Registry 的 `image`，并使用 `${IMAGE_TAG}` 生成版本标签。

## 构建秘密

在部署目标的“环境变量”中新增“秘密值”，勾选“远端构建时作为 BuildKit secret 提供”。秘密保存在 macOS Keychain，同步到共享服务时加密，执行器只在持有任务租约时读取。部署计划与 Git 不包含秘密值。

单容器 Dockerfile 可以使用 `RUN --mount=type=secret,id=PACKAGE_TOKEN ...`。远端 Compose 构建需在 Compose 文件中声明环境型 BuildKit secret，例如：

```yaml
services:
  app:
    image: registry.example/team/app:${IMAGE_TAG}
    build:
      context: .
      secrets:
        - PACKAGE_TOKEN
secrets:
  PACKAGE_TOKEN:
    environment: PACKAGE_TOKEN
```

Compose 构建秘密不得通过镜像构建参数或服务环境变量展开。当前远端 Compose 仅支持构建阶段秘密；运行时 `secrets`、附加构建上下文，以及由本机管理的普通运行环境变量尚未支持。Compose 快照会移除构建配置及构建秘密声明，在共享服务和本机发布历史中加密保存。

## 取消与恢复

桌面端最多等待 10 分钟领取任务，总等待时间最多一小时。取消排队任务会阻止执行器领取；取消运行中的任务会通知执行器停止。如果镜像已推送，服务仍保存可验证的摘要或 Compose 快照。如果推送是否完成无法确认，任务会标记为需要对账。

桌面端重启后，可以从已完成的远端任务恢复单容器摘要或 Compose 快照。Compose 更新、重试或回滚失败时，应用会尝试恢复上一份发布快照。如果入口已更新，但本机无法确认发布记录完整性，应用会保留当前服务并提示核对。已有容器的 Compose 项目需要可恢复的 iDeploy 快照才能安全更新；首次发布可使用未占用的 Compose 项目名。

## 验证边界

仓库测试覆盖 Git 交付、任务租约、密文传输、Compose 配置检查和失败恢复。实际服务器上的 Docker 构建、镜像仓库推送与部署，需要在已配置的共享服务和目标服务器上进行端到端验证。
