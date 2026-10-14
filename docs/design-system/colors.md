# 色彩规范：当前界面

`DeployStyle` 是已使用页面的颜色来源；新组件通过 `AppColors` 的语义别名使用同一套值。下表是当前浅色工作区的精确 SwiftUI RGB 值，不是概念稿取样。

| 用途 | 当前 Token | RGB（0–1） | 使用位置 |
| --- | --- | --- | --- |
| 主文字 | `DeployStyle.ink` | 0.10, 0.15, 0.12 | 标题、列表主行、表单标签 |
| 次文字 | `DeployStyle.muted` | 0.45, 0.52, 0.48 | 说明、元数据、辅助提示 |
| 工作区画布 | `DeployStyle.canvas` | 0.949, 0.961, 0.953 | 主内容背景、对象列表 |
| 主内容面 | `DeployStyle.panel` | 白色 | 卡片、信息分组 |
| 次级内容面 | `DeployStyle.overviewPanel` | 0.976, 0.987, 0.978 | 概览、表单分组 |
| 标题条 | `DeployStyle.overviewHeader` | 0.930, 0.958, 0.936 | 列表和弹窗标题区 |
| 边线 | `DeployStyle.line` | 0.86, 0.90, 0.87 | 分栏、卡片边框、分隔线 |
| 主绿色 | `DeployStyle.green` | 0.16, 0.49, 0.32 | 主按钮、图标、强调文字 |
| 亮绿色 | `DeployStyle.greenBright` | 0.22, 0.64, 0.41 | 选中指示、侧栏局部强调 |
| 浅绿色 | `DeployStyle.greenSoft` | 0.90, 0.96, 0.92 | 选中行、图标底、按压态 |
| 深绿侧栏 | `DeployStyle.nav` | 0.09, 0.14, 0.11 | 全局导航底色 |

全局侧栏在深绿上使用白字及透明度层级；内容区使用 `ink/muted`。成功、警告、失败仍须附文字；现有 `StateBadge` 使用主绿、系统红/橙和次要灰。焦点轮廓应留给系统 `keyboardFocusIndicatorColor`，不要用绿色选中背景替代。

`macos/Assets.xcassets` 中的品牌色是后续外观适配资源；其浅色值应对应此表。当前业务页面大多使用固定 `DeployStyle`，部分页面还强制浅色，因此**深色模式与提高对比度尚未完整支持**。新增组件先维持现有浅色外观；需要启用自适应外观时，应同时迁移文字、画布、卡片、边线和焦点，并实际测量对比度。不要只把一个绿色换成动态色。
