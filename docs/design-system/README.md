# iDeploy 当前界面设计规范

本规范以仓库现有的 macOS 13+ SwiftUI 界面为基准，而非先前的 A/B/C 概念稿。当前工作区的视觉来源是 `macos/Sources/DesignSystem.swift` 中的 `DeployStyle`、`macos/Sources/ContentView.swift` 的主框架，以及部署、服务器、项目、仓库、数据库和设置页面的共同模式。先前对部署页的分栏改造已撤回；部署页维持原布局和行为。

## 使用顺序

1. 阅读[原则](design-principles.md)、[颜色](colors.md)、[字体](typography.md)、[间距和形状](spacing.md)。
2. 根据任务阅读[布局](layout.md)、[组件](components.md)、[导航](navigation.md)、[交互](interaction.md)、[窗口](window-management.md)与[辅助功能](accessibility.md)。
3. 在新 SwiftUI 视图中复用现有 `DeployStyle`、`PageContainer`、`SectionHeading`、`StateBadge`、`EmptyState`、`deployCard()` 和按钮样式；预览组件位于 `macos/Sources/DesignSystem/`。业务字段和操作仍由页面提供。
4. 将[现状审计](ui-audit.md)作为差距清单。它记录了当前尚未实现的深色模式、原生可调分栏等能力，不应把建议误读为已实施规范。

当前外观是**深绿全局侧栏、浅绿画布、白色或淡绿内容面、绿色主操作和紧凑的列表/详情**。已有页面的行为优先于图库示例；图库仅帮助后续组件保持一致。`DeployStyle` 是当前代码中的颜色来源，`AppColors` 为新组件提供同色语义别名。Light/Dark、高对比度与动态文字的完整适配仍需按页面逐步实施和验证，不能仅凭图库预览宣称支持。

[部署页撤回记录](deployments-page-plan.md)说明这次回退的范围。旧概念稿保存在 `concepts/` 供历史参考，不作为新页面的实施依据。
