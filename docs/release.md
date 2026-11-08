# 发布与自动更新

发布由 `Kuangcp/DB-G` 的 GitHub Releases 承担，也是应用内置 mygo Updater 的更新源。
本文维护版本规则、签名密钥、打标签与发布步骤；构建配置在仓库根的 `mygo.json`。

## 版本号

- 正式版标签为 `v<major>.<minor>.<patch>`，例如 `v1.0.1`；应用版本取自标签，`mygo.json` 的
  `version` 只用于本地构建，CI 会在检出副本里改写，不提交版本号改动。
- 每个标签必须在 `CHANGELOG.md` 有 `## <version>` 小节；该小节会进入更新清单，显示在应用的更新窗口。
- 标签必须与 `CHANGELOG.md` 对应；`Release` workflow 校验失败时不会创建构建产物。

## 签名密钥

更新归档用 Ed25519 私钥签名，公钥已写入 `mygo.json` 的 `updates.publicKey`，随安装包分发。
已安装应用只接受同一私钥签名的更新。

密钥位置（本机，权限 0600，**绝不提交**）：

```
~/.config/db-g/update-keys/mygo-update.key   # 私钥，放入 GitHub secret
~/.config/db-g/update-keys/mygo-update.pub   # 公钥，已写入 mygo.json
```

首次配置或换机时，把私钥加入仓库 secret：

```sh
gh secret set MYGO_UPDATER_PRIVATE_KEY < ~/.config/db-g/update-keys/mygo-update.key
```

丢失私钥会使已安装应用无法再收到更新；泄露则可向用户推送任意代码。私钥同时保存在密码管理器里。
重新生成密钥会让旧安装无法更新，只能让用户手动重装。

## 本地构建与验证

开发构建（不签名更新、不写入正式数据目录）：

```sh
make run
```

带签名更新的本地构建（产物在 `dist/`）：

```sh
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/.config/db-g/update-keys/mygo-update.key)" \
  CGO_ENABLED=0 MYGO_LDFLAGS="-X db-g/app.buildVersion=1.0.1 -X db-g/app.releaseBuild=1 -X db-g/app.updateBuild=1" \
  go tool mygo build -platform linux/amd64
```

`mygo build` 会生成安装包、更新归档 `db-g-<version>-<target>.tar.gz` 与
`update-<target>.json`。没有私钥时仍会构建应用，但不生成更新文件；发布 CI 要求更新文件存在。

## 发布步骤

1. 在 `CHANGELOG.md` 写好目标版本的小节，确认 `main` 已包含要发布的提交。
2. 打标签并推送：

   ```sh
   git tag v1.0.1
   git push origin v1.0.1
   ```

3. `.github/workflows/release.yml` 自动执行：
   - **draft-release**：校验标签、CHANGELOG 与 `mygo.json` 更新配置，创建草稿 Release。
   - **build**：`linux/amd64` 与 `windows/amd64` 并行，写入版本/构建标记，
     `go tool mygo build -upload` 构建并用 secret 签名更新，上传到同一草稿。
   - **publish**：核对安装包、更新归档、清单与安装脚本齐全后发布 Release，并设为 latest。
4. 失败时可在 Actions 里 “Re-run failed jobs”；发布前先看草稿资产。标签或 CHANGELOG 有误时，
   修正后删除并重打标签。

## 产物与更新行为

| 文件 | 说明 |
|---|---|
| `db-g-<version>-linux-amd64.tar.gz` | Linux 安装归档（含 `install.sh`），也是更新归档 |
| `db-g_<version>_amd64.deb` | Debian/Ubuntu 包；系统包管理器安装的版本不参与自更新 |
| `db-g-setup-<version>-amd64.exe` | Windows 每用户安装包 |
| `db-g-<version>-windows-amd64.tar.gz` | Windows 更新归档 |
| `update-linux-amd64.json` / `update-windows-amd64.json` | 签名更新清单，应用从这里检查新版本 |
| `install.sh` / `install.ps1` / `SHA256SUMS` / `PKGBUILD` | 安装与校验辅助文件 |

应用启动后由 `plugins/updater/native` 在后台检查新版本；发现更新时显示原生更新窗口，
确认后下载、校验签名、替换文件并在下次启动进入新版本。开发构建与包管理器安装的版本会明确报告
更新不可用。

## 签名与平台提示

- 更新归档的签名由 `MYGO_UPDATER_PRIVATE_KEY` 完成，是自动更新的安全边界。
- 当前没有配置 Windows 代码签名证书或 macOS Developer ID；Windows SmartScreen 可能提示
  “未知发布者”，Linux 无此限制。需要消除系统级提示时，按 mygo 文档在 workflow 中接入
  `WINDOWS_CERTIFICATE` / macOS 公证凭据，这不会改变更新签名的密钥。
