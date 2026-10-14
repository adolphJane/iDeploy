# 组件规范

当前业务页面优先复用下列**已实际使用**的 SwiftUI 组件和样式。`macos/Sources/DesignSystem/` 中的 `App*` 组件是同风格的预览/新视图入口，不能假定旧业务页已使用它们。

| 当前组件 | 规范与用途 |
| --- | --- |
| `PageContainer` | 详情滚动区；30pt 内边距、1080pt 最大内容宽、浅绿画布 |
| `WorkspaceListHeader` | 对象列表标题与“n 个条目”副信息 |
| `SectionHeading` | 18pt 对象/章节标题和 12pt 说明 |
| `InfoTile` | 42pt 浅绿图标底、11pt 标签、20pt 数值，淡绿卡片 |
| `StateBadge` | 10pt 状态文字及低对比度胶囊底，颜色仅辅助语义 |
| `EmptyState` | 58pt 图标底、标题、说明和白色卡片 |
| `deployCard()` / `DeployGroupBoxStyle` | 14pt 圆角、浅绿边线和克制阴影；内容分组 |
| `DeployPrimaryButtonStyle` / `DeploySecondaryButtonStyle` | 绿色主按钮与白底次按钮；按压反馈已定义 |

新组件以同一 Token 和现有尺寸为基准。`AppListRow`、`AppSidebarRow`、`AppStatusBadge`、`AppDetailPanel`、`AppEmptyState` 等可用于后来页面，但复用前要检查真实字段、选中行为、键盘焦点和 VoiceOver。只有存在实际环境字段时才展示 `AppEnvironmentBadge`；当前部署列表不应补造 `production` 标签。编辑、发布、回滚、删除等业务规则留在页面层。
