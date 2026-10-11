# macOS 自动更新发布

iDeploy 的发行包使用 Sparkle 2.10.0。应用启动时读取 GitHub 最新正式 Release 中的 `appcast.xml`，检测、下载并验证更新包。仓库为 public，应用可匿名读取更新源；框架从 Sparkle 官方 Release 下载，构建脚本固定了发行包的 SHA-256。用户无需配置更新地址或开关。

## 一次性发布准备

1. 保持仓库为 public，使 `https://github.com/adolphJane/iDeploy/releases/latest/download/appcast.xml` 和 Release ZIP 可以由未登录的用户访问。当前最新正式版 `v0.5.0` 尚未包含 `appcast.xml`；首次发布带 Sparkle 的版本后，GitHub 的 latest 下载地址才会提供更新清单。私有仓库的 Release 无法直接用于当前无凭据更新方案；不要将 GitHub Token 嵌入应用。
2. Sparkle 公钥保存在 `build/darwin/sparkle-public-key.txt`。对应私钥已在本机 macOS Keychain 的 `com.ideploy.app` Sparkle 账户中生成，不能提交到仓库。请使用同一密钥导出供 Actions 使用：

   ```bash
   umask 077
   /path/to/Sparkle/bin/generate_keys --account com.ideploy.app -x /tmp/ideploy-sparkle-private-key.txt
   ```

   将导出文件的完整内容设为仓库 Actions Secret `SPARKLE_ED25519_PRIVATE_KEY`，随后删除导出文件。GitHub Actions 构建时会确认私钥与仓库公钥匹配；缺少或不匹配会使发布失败。此密钥是发布可执行更新包的凭据，请做好离线备份。
3. 当前构建使用 ad-hoc 代码签名；正式对外分发建议增加 Developer ID 签名及公证。Sparkle 的 Ed25519 更新签名与 Apple 代码签名是两套独立机制。

## 每次发布

将新的 `vX.Y.Z` 标签推送到 GitHub。工作流构建 Universal `.app`、嵌入并签名 Sparkle.framework，设置递增的 `CFBundleVersion`，制作 ZIP，再生成包含 ZIP 地址和 Ed25519 签名的 `appcast.xml`。三个附件一起上传到同一个 Release。`latest/download/appcast.xml` 始终指向最新正式版；预发布标签不会通过该地址推送。

旧版 iDeploy 没有 Sparkle，必须手动安装一次带更新器的版本。此后应用每次冷启动立即检查；后台下载的更新通常在应用真正退出时安装。关闭窗口只会隐藏 iDeploy，菜单栏中的“退出”才会结束进程。需要写入权限时，macOS 可能要求用户授权。

如果发布任务缺少 `SPARKLE_ED25519_PRIVATE_KEY`，不要绕过签名检查。修复 Actions Secret 后以新的版本标签重新发布。
