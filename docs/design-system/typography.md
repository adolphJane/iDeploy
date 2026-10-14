# 字体与排版：当前桌面密度

当前界面使用系统 SF 字体，局部以固定字号构成下表。`AppTypography` 为新组件提供对应层级；现有页面暂不批量重写。

| 层级 | 当前大小/字重 | 典型位置 |
| --- | --- | --- |
| 工作区标题 | 26pt Bold | 顶部 92pt 工作区标题区 |
| 详情标题 | 18pt Bold | `SectionHeading` |
| 列表标题 | 16pt Bold | `WorkspaceListHeader` |
| 卡片标题 | 14pt Semibold | `DeployGroupBoxStyle` |
| 列表主行/表单标签 | 12pt Semibold | 对象名称、字段名 |
| 正文/操作文字 | 12pt Regular/Medium | 表单说明、按钮 |
| 辅助文字 | 11pt Regular | 次级信息、注释 |
| 紧凑元数据 | 10pt Regular/Semibold | 列表副行、状态徽标 |
| 技术内容 | 系统等宽 11pt 或原生 `.monospaced` | 镜像、路径、端口、版本 |

标题只在所属层级使用；状态、按钮和普通说明不擅自放大。长路径与技术值允许复制和换行，不能靠无限缩小字体维持单行。10–11pt 对放大文字和高对比度有风险；迁移动态字级时须连同列表行高、表单宽度及布局一起验收，不能只替换 `Font`。
