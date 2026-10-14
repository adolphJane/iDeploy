# 部署页改造撤回记录

先前按 A 布局、C 配色、B 标签制作的部署页实验已按用户要求撤回。`macos/Sources/DeploymentsPage.swift` 恢复仓库原有的手工列表/详情、原有颜色、弹窗和发布/回滚等操作。旧概念稿保留在 `concepts/`，仅作历史参考。

现行实现以 `DeployStyle`、`WorkspaceListHeader`、`PageContainer`、`SectionHeading`、`InfoTile`、`StateBadge`、`deployCard()` 及按钮样式为准。当前部署数据没有环境名称字段，规范不会要求页面展示 `production/staging` 标签。

后续界面调整使用[当前界面设计规范](README.md)，先确认单页方案，再修改页面；不将已撤回的分栏方案视为既定目标。
