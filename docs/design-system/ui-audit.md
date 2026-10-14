# iDeploy macOS 原生 UI 审计

日期：2026-10-14
范围：`macos/Sources/` 当前 SwiftUI 界面。
方法：静态阅读界面源码、应用包配置和构建脚本。界面捕获工具本轮无法连接本机 App，因此本文不把视觉渲染、实际窗口缩放、VoiceOver 朗读或键盘焦点行为写成已验证事实。

当前决策：部署页实验已撤回；下文的“设计方向”列是审计时提出的可选改进，并非现行布局要求。实际新增 UI 以[当前界面设计规范](README.md)为准。

## 1. 项目和界面地图

| 层级 | 当前实现 | 证据 |
| --- | --- | --- |
| 应用入口与窗口 | SwiftUI `WindowGroup("iDeploy", id: "main")`、独立 `Settings` scene；AppKit `NSStatusItem` 菜单栏入口 | `macos/Sources/iDeployApp.swift:3–89` |
| 主工作区 | `ContentView` 手工 `HStack`：226pt 全局侧栏、92pt 标题/刷新区、当前页面 | `macos/Sources/ContentView.swift:4–217` |
| 页面 | 总览、部署、服务器、项目、镜像仓库、数据库、设置；由 `AppModel.section` 切换 | `macos/Sources/ContentView.swift:15–31`、`macos/Sources/AppModel.swift:5–31` |
| 业务页面模式 | 部署/服务器/项目/仓库/数据库以手工列表＋详情为主；总览为卡片和任务列表 | `macos/Sources/{DeploymentsPage,ServersPage,ProjectsPage,RegistriesPage,DatabasesPage,ContentView}.swift` |
| 现有样式 | `DeployStyle` 颜色、卡片、GroupBox、两种按钮样式与列表标题 | `macos/Sources/DesignSystem.swift:1–100` |
| 平台目标 | macOS 13.0；Swift 编译目标也为 macOS 13.0 | `build/darwin/Info.plist:23–24`、`build/darwin/build-native.sh:25,33` |

当前已有可复用的页面容器、标题、状态徽标、空态及按钮样式；原生 `Button`、`Picker`、`TextField`、`Form`、`GroupBox`、`Menu`、`Settings` scene 和 `confirmationDialog` 也已经使用。业务模型、Go Runtime、CLI、MCP 不属于本次视觉改造范围。

## 2. 逐项检查

| 检查项 | 现状与判断 |
| --- | --- |
| NavigationSplitView / NavigationStack | 未使用。全局侧栏、工作区列表和详情由嵌套 `HStack` 与页面状态手工管理；多列结构已形成，但缺少系统分栏的调整与收起行为。 |
| Sidebar | 全局侧栏固定 226pt、深色渐变；业务页面列表栏通常 240–330pt。选中样式由每页分别实现。设置页又有一套分类侧栏。 |
| Toolbar | 未见 SwiftUI `.toolbar`。顶部 92pt 自绘 `workspaceHeader` 承载标题与刷新；新增、发布、编辑等操作放在页面内容或列表标题中。 |
| Menu Commands | `CommandMenu("操作")` 包含刷新和总览/部署/服务器/项目四个快捷键；`CommandGroup` 去掉“新建”。仓库、数据库、设置以及当前对象的操作没有对应的显式命令。 |
| Context Menu | 当前源码未见 `.contextMenu`。列表对象的编辑、删除、运行操作主要进入详情后查找。 |
| Settings 窗口 | 存在原生 `Settings` scene，但主侧栏也可显示相同 `SettingsPage`；SettingsPage 内再有分类侧栏，形成两种入口和两层导航。 |
| 颜色和字体 | 11 个基础色为固定 RGB/白色，页面内仍有额外 `Color(red:)`、`.white` 和多处局部字号。系统语义色只部分用于状态与正文。 |
| 间距/形状/边框/阴影 | `PageContainer` 统一 30pt 内边距；多处使用 10、12、14、16、18、20、24 等数字。卡片默认 14pt 圆角、22pt 模糊阴影，列表/表单另有局部值。 |
| 组件复用 | `PageContainer`、`SectionHeading`、`InfoTile`、`StateBadge`、`EmptyState` 等已复用；五个工作区的列表行、选中标记、分割线、空详情重复编写。 |
| Dark Mode | 设置页和多个业务页/表单明确 `.environment(\.colorScheme, .light)`；固定浅色画布和白卡片无法随系统切换。当前不满足深色模式目标。 |
| 键盘快捷键 | 有 ⌘R、⌘1–4，以及多张表单的默认/取消动作；其余主要业务操作没有统一快捷键和菜单映射。 |
| Hover 与焦点 | 未见 `.onHover`、`@FocusState`、`.focusable` 或自定义焦点反馈；原生控件可能保留系统焦点，但 `.plain` 的自绘列表行需运行态验证。 |
| 窗口缩放 | 主窗口最小 1000×670、默认 1260×800；设置最小 740×600。部分网格可伸缩，但总览三列、固定双栏/内层 235pt 侧栏、固定 sheet 宽高存在窄窗口和大文字风险。 |
| VoiceOver | 部分列表行标记 `.isSelected`、任务行有组合标签，仓库浏览使用隐藏不可见层；其余自绘图标按钮、状态点及动态反馈的朗读顺序与名称尚未验证。 |
| 鼠标和拖放 | 原生 Button、Menu、滚动及表格排序可用；未见统一悬停、双击、右键菜单或拖放反馈。不能因源码未实现就假定用户需要拖放，须按任务语义决定。 |
| 加载、错误、确认 | 主窗口有后台连接进度和全局错误 alert；各页有空态、sheet、confirmationDialog 和任务进度。反馈样式与摆放分散。 |

## 3. 主要发现（按优先级）

| 优先级 | 发现、证据与影响 | 设计方向 |
| --- | --- | --- |
| P0 | **深色模式被主动关闭。** 设置页、数据库/仓库/项目等页面及表单强制浅色；`DeployStyle` 固定 RGB/白色。系统外观、高对比度、强调色无法可靠传递到自绘区域。证据：`DesignSystem.swift:3–17`，`SettingsPage.swift:118`，`DatabasesPage.swift:177,806`，`ProjectsPage.swift:130,414`，`RegistriesPage.swift:222,304,975`，`ServersPage.swift:430`，`DeploymentsPage.swift:451`。 | 先建立语义色和 Light/Dark/高对比度方案，再逐页去掉强制外观；保留状态语义与系统强调色。 |
| P1 | **桌面分栏被重复手工实现。** 全局侧栏及五个业务页面列表/详情都以 `HStack`、`ScrollView`、自绘选中条实现；列表键盘导航、分栏宽度和窗口缩小时的行为需由项目自行维护。证据：`ContentView.swift:8–31,116–149`；各业务页开头约 100 行。 | 对适合三级结构的页面评估 `NavigationSplitView` + `List(selection:)`；仅在业务确有特殊数据密度时保留自绘列表。 |
| P1 | **工具栏与主窗口层级不符合当前 macOS 工作方式。** 92pt 自绘标题区承载刷新，页面一级动作位置不同；缺少统一 `.toolbar`、搜索与对象级次要操作入口。证据：`ContentView.swift:151–193`，`DeploymentsPage.swift:84–104`。 | 将全局标题、刷新、搜索和当前对象操作分配到系统 Toolbar/页面内容；避免重复动作。 |
| P1 | **窗口与 sheet 尺寸偏硬。** 主窗口至少 1000pt 宽、设置 740pt，表单常固定 650×540/760 等；数据库浏览器还在详情内固定 235pt 表栏。大文字、窄显示器与缩放边界可能造成挤压或内容裁剪，实际程度待运行测试。证据：`iDeployApp.swift:59–86`，`ProjectsPage.swift:413`，`DatabasesPage.swift:303,805`。 | 定义最小/理想/宽屏断点和内容优先级；表单内容滚动且底部动作稳定，分栏可调整。 |
| P1 | **设置有两条导航路径。** 既是独立 `Settings` scene，又是主工作区 `AppSection.settings`；内容内还有一层分类栏。用户可能在两个窗口中看到不同的设置导航状态。证据：`iDeployApp.swift:84–88`，`ContentView.swift:29,94–96`，`SettingsPage.swift:50–114`。 | 在后续页面改造中统一入口和窗口职责，保留现有设置能力及快捷访问。 |
| P2 | **视觉参数集中度不足。** `DeployStyle` 只有颜色及少量样式；字号、间距、圆角、分割线和阴影仍散落在近 5,000 行 SwiftUI 中。例：`ContentView.swift:196–265`、`SettingsPage.swift:307–355`、`DatabasesPage.swift:300–354`。 | 建立少量语义 Token（颜色、字级、间距、形状、动效），按用途而非数字命名，逐页迁移。 |
| P2 | **列表与反馈组件有重复。** 项目、服务器、部署、仓库、数据库都复制“标题＋添加按钮＋列表行＋选中条＋空详情”的模式；错误、连接状态和加载反馈则在页面内各自拼装。证据：`ProjectsPage.swift:15–146`，`ServersPage.swift:16–214`，`DeploymentsPage.swift:18–212`，`RegistriesPage.swift:35–124`，`DatabasesPage.swift:23–169`。 | 抽取满足当前两类以上页面需要的行、标题、状态与空态；保持业务字段由调用方提供。 |
| P2 | **字号和信息密度不稳定。** 10–11pt 文本用于侧栏辅助信息、状态、表单说明和列表元数据；概览卡片与 26pt 页面标题则占较多纵向空间。证据：`ContentView.swift:65–107,155–160,240–265`，`SettingsPage.swift:70–84`。 | 建立桌面字级并检验系统文字放大；压缩装饰区，优先给列表和详情留空间。 |
| P2 | **键盘/鼠标操作不完整。** 菜单只覆盖四个导航区，列表没有显式右键菜单、方向键选择或双击约定，危险与次要动作位置不一致。证据：`iDeployApp.swift:72–83` 及各业务页列表实现；源码搜索未见 `.contextMenu`/`.onHover`。 | 为高频操作建立命令表和焦点/选中模型；右键菜单复用当前对象操作，悬停只补充不替代焦点。 |
| P2 | **辅助功能覆盖不均。** 有选中 trait 与任务行朗读标签，但小字号和固定颜色是高风险；自绘状态点、纯图标按钮、动态提示、复杂网格需要实测。证据：`OverviewJobs.swift:154–155`，`ContentView.swift:99–107,148`，`DatabasesPage.swift:155,747`。 | 对每个组件定义名称、值、状态、焦点、操作和公告；在 Light/Dark/高对比度与 VoiceOver 下验证。 |

## 4. 窗口、导航与交互的具体边界

- **多窗口：** `WindowGroup` 允许多个主窗口，但 `AppModel` 在 `App` 层仅创建一个 `@StateObject`，`section` 等导航状态由它持有；若打开多主窗口，导航状态可能联动。`NativeAppDelegate.openWindow()` 通过 `NSApp.windows.first` 寻找窗口，尚未体现明确的多窗口策略。应先决定主工作区单窗口还是多窗口，再设计恢复与焦点。证据：`iDeployApp.swift:39–86`。
- **设置：** 原生 `Settings` scene 是好的基础；当前复用同一 `SettingsPage` 到主导航，可能让设置页的窗口尺寸、层级和命令行为不一致。证据：`ContentView.swift:29`、`iDeployApp.swift:84–88`。
- **详情：** 项目、服务器、部署等使用列表/详情比单列长页更适合桌面；但详情内部继续把各区块做成卡片，增加滚动和视觉分隔。优先保留内容分组，减少不必要的背景与阴影。
- **危险操作：** 删除、回滚等已有 `confirmationDialog`，这是可保留的保护；后续需核对提示是否说明作用范围、焦点默认位置与关闭方式。本文未操作真实业务数据。

## 5. 与 Apple 指引的关系

Apple 的 [macOS 设计指南](https://developer.apple.com/design/human-interface-guidelines/designing-for-macos/)强调可调整窗口、高信息密度、菜单栏与键盘工作流；[颜色指南](https://developer.apple.com/design/human-interface-guidelines/color)建议语义色适配浅色、深色和高对比度；[深色模式指南](https://developer.apple.com/design/human-interface-guidelines/dark-mode)建议尊重系统外观；[设置指南](https://developer.apple.com/design/human-interface-guidelines/settings)描述 macOS 设置窗口的持续可见导航；[SwiftUI NavigationSplitView 文档](https://developer.apple.com/documentation/swiftui/navigationsplitview)说明系统分栏能力。以上是评估依据，不代表所有页面必须套用同一种系统控件。

## 6. 已验证范围与后续验收

本轮已确认目录、场景声明、窗口尺寸、导航结构、样式参数、可访问性修饰符和页面操作位置。未进行运行态截图或行为测试；因此未判定实际对比度比值、窗口裁剪程度、VoiceOver 朗读顺序、Tab/方向键路径、Reduce Motion 反应和真实 Hover 状态。后续逐页调整时，应在原生应用中分别验证浅色/深色/高对比度、1000pt 最小宽度与更宽窗口、系统文字放大、键盘导航、VoiceOver 和鼠标右键流程。
