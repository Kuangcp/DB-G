# 数据源配置 Git 同步设计

> 状态：已实现（P1–P3；冲突取舍为安全中止）。参考 `db-k` 的 `doc/superpowers/specs/2026-11-04-git-config-sync-design.md` 与
> `src/main/kotlin/sync/*`，按 db-g 的存储与分层重做，不复用其口令派生方案。

## 1. 结论与范围

需求合理：多设备共用数据源与控制台 SQL，最省事的传输层就是 git；复用系统 `git`/`ssh` 能直接吃到
用户已有的私钥、agent、credential helper，不需要内置 SSH 库或 JGit。

但有两处需要按 db-g 现实调整：

1. **三条路径要分清**：首次初始化是 `clone → checkout 配置分支 → 解析导入`；后续拉取是
   `fetch/merge → 解析导入`；提交是「本地快照写入仓库并 commit → fetch/merge（冲突则中止）
   → push」。本地未提交的 DB 改动属于提交流程，拉取以远端为准；工作树有未提交文件时拉取直接拒绝。
2. **本地 AES 密钥要解决跨设备分发与错钥防护**。密钥不进仓库，另一台设备没有同一个密钥就解不开连接。
   因此不能「没有密钥就一律生成新密钥」：仓库里已有密文时必须让用户导入已有密钥，
   否则新密钥只会让同步永久失效。仓库里放 **KCV**（Key Check Value），
   拉取/提交前先比对，确保本地密钥与仓库数据匹配；不匹配直接中止，绝不解密后回写。见 §5。

### 同步什么

| 同步 | 不同步（设备本地） |
|---|---|
| 连接档案 `connections.json` 的 conns（整条加密） | 密码本机密文 `secret.key`、活动连接/工作区指针 |
| 文件夹 folders | 执行历史 `history/`、元数据缓存 `meta/` |
| 工作区 workspaces（成员顺序） | 控制台光标位置、会话状态 |
| 内部控制台元数据 + 正文 `consoles/*.sql`（明文） | 外部文件控制台（路径是设备本地的） |
| 控制台排序 `sortOrder` | `ai/`、`logs/`、`theme.json`、`fonts.json`、`keymap.json`、`ai.json`、`mcp.json` |

“配置”按 `connections.json` 的结构化字段理解；主题/快捷键/AI 等偏好不在本期范围。

### 非目标

- 不做自动定时同步、退出自动推送、后台常驻。
- 不做多分支 UI、历史回滚 UI（交给 `git log`）。
- 不内置 SSH/JGit，不实现 OAuth、账号体系。
- 不把外部 `.sql` 内容复制进仓库（会让本地文件与仓库副本互相打架）。
- 不加密控制台 SQL（用户明确要求明文，换取可读 diff 与文本合并）。

## 2. 与 db-k 的差异

| 维度 | db-k | db-g 本设计 |
|---|---|---|
| 加密密钥 | 用户口令 + 仓库 `salt.bin`，PBKDF2 派生 | 本机随机 AES-256 密钥文件 `sync.key`，仓库 `key.kcv` 做 KCV 校验 |
| 密钥输入 | 首次输入口令，记住后不再问 | 首次生成或导入；缺失时弹窗，导入/生成后存 `sync.key` |
| 错钥防护 | `key.kcv` 明文比对 | KCV 明文比对 + GCM tag 双保险，比对失败不碰本地与 git |
| 密钥丢失 | 重新输入口令即可恢复 | 不可恢复（只能从备份/另一台设备导入）；UI 必须明确警告 |
| 加密范围 | 整条连接条目 | 同上：整个 `ConnProfile` JSON（含 name/host/port/user/password/path/params/tags） |
| SQL | 明文 | 明文 |
| 冲突 | 按实体整份取舍（已实现） | 一期安全中止；二期补取舍弹窗（见 §10） |
| 配置 | `sync.properties` | `sync.json`（与其他 prefs 一致） |
| 仓库命名空间 | `<repo>/db-k/` | `<repo>/db-g/`，同一仓库可与其他应用共存 |

## 3. 分层与组件

```mermaid
flowchart LR
  UI[ui/sync*.go<br/>设置页/密钥弹窗] --> APP[app/sync*.go<br/>适配器的落盘与导入]
  APP --> SYNC[sync/<br/>纯 stdlib：git 封装 / 加密 / 模型 / 引擎]
  SYNC --> GIT[系统 git + ssh]
  APP --> STORE[(connections.json<br/>consoles/*.sql)]
```

| 层 | 文件 | 职责 |
|---|---|---|
| `sync/` | `git.go` | 系统 git 薄封装（exec、超时、环境变量、统一错误） |
| | `secret.go` | AES-GCM 加解密、KCV、密钥文件读写 |
| | `paths.go` | 仓库目录 ↔ 实体 id；`db-g/` 命名空间 |
| | `models.go` | 同步 DTO（`ConnSync`/`FolderSync`/`WorkspaceSync`/`ConsoleMetaSync`/`ConflictItem`） |
| | `engine.go` | 导出工作树、提交、pull/merge、push、冲突检测；不 import app |
| `app/` | `syncservice.go` / `syncservice_apply.go` | `ConnStore` + 控制台文件 ↔ `sync.Snapshot`；批量导入（单次 save） |
| | `syncprefs.go` / `syncstore.go` | `sync.json` 读写、密钥路径、`ApplySync` 与 `ResetForSync` |
| | `secretscan.go` | 控制台 SQL 明文凭据扫描与 HMAC 指纹 |
| `ui/` | `sync_settings.go` | 设置页「同步」分区、密钥弹窗、敏感信息确认、拉取/提交按钮与状态 |
| | `sync_refresh.go` | 同步完成后关闭失效会话、刷新树/控制台/编辑器 |

import 方向：`ui → app → sync`，`sync` 不 import `app/ui/engine`。AGENTS 分层表加一行。
所有 git/IO/加解密都在后台协程；结果用 `win.Update` 回投（AGENTS「线程与并发」）。

## 4. 仓库布局

```
<repo>/
  README.md                         # 生成：说明 db-g/ 下 *.enc 是密文，勿手改；SQL 为明文
  db-g/
    key.kcv                         # Key Check Value（明文），比对本地密钥是否匹配仓库
    folders/<folderID>.json
    connections/<connID>.enc        # 整个 ConnProfile JSON 的 AES-GCM 密文
    workspaces/<workspaceID>.json   # name/autoNamed/sortOrder/createdAt/lastActiveConsoleID/members[]
    consoles/<consoleID>.meta.json  # id/connectionID/name/sortOrder/createdAt/updatedAt（不含 filePath/caret）
    consoles/<consoleID>.sql        # 正文，明文
```

规则：

- 一个实体一个文件，冲突粒度 = 一个连接/一个控制台；不要整份 `connections.json` 进仓库。
- **id 即文件名**；重命名不换 id。
- `ActiveID`/`ActiveWorkspaceID`/`CaretStart`/`CaretEnd`/`FilePath`/`External` 不进仓库；
  导入时按 id 重算 `FilePath`，外部控制台保持本机记录不动。
- 工作树固定为 `<AppConfigDir()>/sync/repo`（`db-g`/`db-g.dev` 自动隔离）。
- 仓库根 `.gitattributes` 由程序维护（只追加缺失行）：`db-g/**/*.enc -text`、`db-g/**/*.sql text eol=lf`。
- JSON 用 `json.MarshalIndent`、稳定字段顺序；id 排序后写入，保证 diff 稳定。
- `README.md` 明确：仓库包含控制台 SQL 明文，含敏感 SQL 请用私有仓库。

### 连接条目明文 JSON（加密前）

```json
{
  "formatVersion": 1,
  "id": "a1b2c3d4",
  "name": "dev-pg",
  "kind": "postgres",
  "host": "127.0.0.1",
  "port": 5432,
  "database": "orders",
  "user": "app",
  "password": "…",
  "params": "sslmode=disable",
  "queryTimeout": 30,
  "guardBlock": false,
  "tags": [{"label": "dev", "color": "blue"}],
  "folderID": "e5f6a7b8"
}
```

## 5. 密钥与加密

### 5.1 本机密钥文件

- 路径：`<AppConfigDir()>/sync.key`，32 字节随机，`0600`。
- 设置页状态：`已配置` / `未配置`；操作：生成、复制（base64）、导入（文件或粘贴 base64）、清除。
- 密钥只在内存与 `sync.key` 出现：不写日志、不进仓库、不进 git 命令参数。
- 首次生成时提示“请自行备份；丢失后仓库里的连接无法解密”，并提供复制。
- `sync.json` 另存 `keyKCV`，用于在完全离线时发现“密钥文件被替换”这类低级错误；
  **仓库里的 KCV 才是比对基准**，本地记录只用于提示。

### 5.2 KCV（错钥防护）

目的：另一台设备用错误密钥拉取时，必须在读入/回写任何数据之前明确失败；
不能出现“解出来一团垃圾还继续同步”的情况。

- 算法：标准 KCV，`KCV = AES-256-ECB(key, 16 个 0 字节) 的前 3 字节`，写作 6 位大写十六进制。
  KCV 不参与任何数据加密，只做快速核对；24-bit 碰撞概率对“误操作检测”足够，
  真正的密码学校验仍是每个文件的 GCM tag。
- 仓库文件：`db-g/key.kcv`，内容是 `A1B2C3\n` 这样的明文。它不泄露密钥。
- 校验时机：
  - `ensureRepo` 之后、任何解密/写工作树/commit 之前，先读 `db-g/key.kcv` 与本地密钥的 KCV 比较；
  - 不一致 → 报「同步密钥与仓库不匹配」，终止；本地连接库与工作树都不动；
  - 仓库还没有 `key.kcv`（首次使用）→ 当前密钥就是仓库密钥，首次推送时写入；
  - 仓库有 `.enc` 却没有 `key.kcv` → 拒绝同步并提示仓库损坏/版本不受支持，不猜。
- 导入密钥时：先算 KCV；仓库已有 `key.kcv` 且不一致 → 直接拒绝导入，不保存；
  一致才写入 `sync.key` 与 `sync.json.keyKCV`。
- 更换密钥：必须重加密全部 `.enc` 并同步更新 `key.kcv`，本设计首版不做；
  清除密钥后重新导入正确密钥即可恢复。

### 5.3 仓库内密文

- 算法：AES-256-GCM，12B 随机 nonce，128-bit tag。
- 文本格式：`dbg-sync:v1:<base64(nonce||ciphertext||tag)>`，便于肉眼识别为密文、避免被当作 JSON 解析。
- **去抖**：GCM 随机 nonce 使相同明文每次密文不同。导出 `connections/<id>.enc` 前先解密仓库现有文件，
  与本次明文 JSON 字符串比较，相同则不写；否则每次同步都会产生无意义提交。
- 不加密 SQL、文件夹/工作区/控制台元数据（不含连接机密；便于 diff 与合并）。
- 导入时校验明文里的 `id` 与文件名一致，防止有人把两个 `.enc` 文件互换。

### 5.4 与本地密码存储的衔接

- 导出：`ConnProfile.Password` 在 `ConnStore` 内存里是明文 → 序列化整条 JSON → 用同步密钥加密。
- 导入：解密 → 得到明文 `ConnProfile` → `ConnStore` 落盘时仍用本机 `secret.key`（`Vault`）加密密码。
- 两个密钥互不替代：`secret.key` = 本机静态保护，`sync.key` = 跨设备仓库保护。

### 5.5 密钥缺失/错误的交互

拉取与提交前都执行 `ensureSyncKey(repoState)`：

| 本地 `sync.key` | 仓库 `key.kcv` | 行为 |
|---|---|---|
| 有 | 无 | 用现有密钥；首次推送写 KCV |
| 有 | 有且 KCV 一致 | 正常 |
| 有 | 有但 KCV 不一致 | 报「同步密钥不匹配」，不改本地、不 commit、不 push |
| 无 | 无 | 弹窗：生成新密钥（保存 `sync.key`）或导入已有密钥 |
| 无 | 有 | **只能导入**已有密钥（粘贴/选文件），导入时用 KCV 校验；禁止生成 |

仓库状态检查需要工作树存在，因此流程是：先 `ensureRepo`（clone 或 init），再检查密钥。
推送时若远端已有 `key.kcv` 而本地无密钥，提示「先导入密钥并拉取」，不执行 push。

## 6. Git 后端

### 6.1 调用约定

- 复用系统 `git`（`exec.CommandContext`），不引入 JGit/SSH 库；启动时 `git --version` 探测。
- 工作树目录不存在时创建；`git init -b main`；本地身份只在仓库未配置时写
  `user.name=db-g`、`user.email=db-g@localhost`。
- 所有命令：
  - `-c core.quotepath=false`（中文文件名不转义）；
  - `GIT_TERMINAL_PROMPT=0`、`GIT_ASKPASS=true`，stdin 关闭，绝不进入交互；
  - `GIT_SSH_COMMAND=ssh -i <私钥路径> -o IdentitiesOnly=yes -o BatchMode=yes`（仅当设置了私钥路径）；
  - 超时 120s（push/pull/clone），输出截断 64 KiB，stderr 合并。
- 私钥路径留空 = 用系统 ssh 默认行为：`~/.ssh/config`、`ssh-agent`、默认 `id_*`。
- 私钥有口令且没有 agent 时 `BatchMode` 会失败；错误文案提示“请先 `ssh-add`，或改用无口令部署密钥”。
- 不做 `StrictHostKeyChecking=no`；host key 未信任时明确报错，让用户走正常 `ssh` 流程。

### 6.2 配置（`sync.json`）

```json
{
  "repoURL": "git@github.com:me/db-g-sync.git",
  "branch": "main",
  "sshKeyPath": "~/.ssh/id_ed25519",
  "keyKCV": "A1B2C3",
  "acceptedFindings": ["<hmac>", "…"],
  "lastSyncAt": 1763100000000
}
```

- 默认 `branch = "main"`；`sshKeyPath` 支持 `~` 展开。
- `keyKCV` 是本机密钥的指纹（见 §5.2），用于离线提示密钥被替换；`acceptedFindings` 是
  敏感扫描已确认项的 HMAC 指纹（见 §9），只存指纹不存原文。
- 工作树固定，不出现在配置里；设置页只展示只读路径。
- 不存密钥、不存仓库用户名/密码。

## 7. 两个动作

### 7.1 提交（push）

```
1. 校验配置（repoURL 非空、git 可用）与密钥（§5.5：KCV 必须一致）
2. 刷新编辑器：flushActive + consoleMgr.FlushAll()（AGENTS 约定）
3. adapter.Snapshot()          // 读内存/磁盘的 conns/folders/workspaces/consoles(内部控制台)
4. 生成文件清单（内存）：engine.BuildFiles(key)  // 写/删 db-g/、.enc 明文比对去抖
5. 敏感信息扫描：对清单里的 *.sql 跑 §9；有命中且未确认 → 中止，不写盘/不提交
6. engine.WriteFiles() + commit("db-g sync: <hostname> <ISO 时间>")
   - 无变更：跳过 commit，但仍继续 merge/push
7. 空库/首次守卫：
   - 本地连接为空而远端 db-g/ 有连接 → 拒绝推送
   - 本机从未成功同步（lastSyncAt=0）而远端已有 db-g/ 数据 → 要求先拉取导入
8. fetch + merge origin/<branch>
   - 冲突：git merge --abort → 返回冲突文件列表，不 push
   - 远端还没有该分支：跳过 merge，直接 push（首次建分支）
9. 合并产生了新提交：读取合并后的工作树 → 解析 → Apply 到本地（让应用与仓库一致）
10. git push origin HEAD:refs/heads/<branch>
    - non-fast-forward → 提示「远端有新提交，请重试提交」
11. 记录 lastSyncAt、文件计数，刷新状态
```

### 7.2 拉取（pull）

```
1. ensureRepo：
   - 工作树没有 .git → git clone <repo> → checkout <branch>
   - 已有 .git → 更新 origin URL → fetch → checkout <branch>
   - 远端为空仓库：clone 后创建同名本地分支；拉取只返回「已初始化」
2. 校验配置与密钥（§5.5，需要工作树才能读 key.kcv）
3. 工作树有未提交改动 → 拒绝拉取，提示先提交（避免半状态）
4. fetch + merge origin/<branch>
   - 首次 clone 后一般 Already up to date
   - 冲突：git merge --abort → 返回冲突路径，本地库不动
5. 读取合并后的工作树：解密/解析全部实体；任一连接解不开 → 整体失败，不写本地
6. adapter.Apply(plan)：校验引用 → 写控制台正文 → 单次批量替换 store → 清理被删的正文文件
7. ui 刷新：关闭已删除/已变更连接的会话、重建树与标签、修正活动工作区/控制台
8. 记录 lastSyncAt
```

关键点：

- 首次使用空仓库：clone 会成功（git 给出 empty repository 警告），随后 checkout/创建配置分支；
  不把“远端没有分支”当失败。
- 配置分支在远端不存在但仓库有其他分支：明确报「远端没有分支 X」，不悄悄建空分支。
- 拉取不替用户提交本地 DB 改动；工作树脏时直接拒绝，避免把半成品提交进历史。
- 第 5 步必须在第 6 步之前完成全部解密与 JSON 解析；不能“边解析边写”。
- 合并结果里出现 `<<<<<<<` 的 `.sql` 绝不导入：冲突路径应先被第 4 步拦下。

## 8. 导入语义（app 层）

新增 `app` 方法（避免逐条 `Add/Update/Remove` 触发多次写盘）：

```go
// SyncSnapshot 是导出侧的全量同步态（明文）。
type SyncSnapshot struct {
    Conns      []ConnProfile
    Folders    []ConnFolder
    Consoles   []ConsoleRecord // 仅内部控制台
    Workspaces []WorkspaceRecord
}

// ApplySync 用合并结果替换本地连接库；外部控制台与设备本地状态保留。
func (s *ConnStore) ApplySync(plan SyncPlan) error
```

`ApplySync` 规则：

1. 校验引用：控制台的 `ConnectionID` 必须存在，否则跳过并计数；文件夹 `ParentID`、工作区成员同理。
2. 保留本地外部控制台（`External=true`）及其正文文件，不因拉取被删。
3. 已存在的控制台保留本机 `CaretStart/CaretEnd`；`FilePath` 按 id 重算；`External=false`。
4. 保留本地 `ActiveID`/`ActiveWorkspaceID`，指向已删除实体时回退到空/第一个有效项。
5. 单次快照 + 一次 `save()`；失败回滚内存状态（复用现有 `snapshot/restore`）。
6. 正文文件：内容不同才写；被移除的内部控制台在 store 保存成功后删文件；
   外部文件永不删除。
7. 导入后的 `ConnStore` 仍需用本机 `Vault` 加密密码后落盘（沿用 `save()`）。

UI 刷新（同步成功后）：

- 对「已删除连接」和「连接信息已变化」的会话调用 `sessionFor` 的 `Close` 并从 `sessions` 移除；
- 若当前控制台被删除，清空编辑器并切到有效控制台/空态；
- 清空 catalog 缓存、重新加载树与当前控制台正文。

## 9. 控制台 SQL 敏感信息扫描

控制台 SQL 明文进仓库，需要一层“防止误传密码”的护栏；目标是**高精度拦截 + 一次确认**，
而不是做通用密钥扫描器把用户烦死。

### 9.1 扫描对象与时机

- 对象：即将写入仓库的所有 `.sql`（`BuildFiles` 的内存结果），不读用户外部文件。
- 时机：提交（push）与拉取前的本地快照提交都执行；**在写工作树与 commit 之前**。
  取消确认 = 不写、不 commit、不 push，本地配置与仓库都不变。
- 已扫描命中的记录按 fingerprint 存入 `sync.json`：`HMAC-SHA256(sync.key, 路径 + 规则 + 命中行)`，
  不存原值（避免对低熵密码做离线字典反推）；同一个未变动的命中不再重复弹窗。
  命中行发生变化后重新提示。

### 9.2 规则（高精度优先）

| 规则 | 示例 | 置信 |
|---|---|---|
| PEM 私钥块 | `-----BEGIN OPENSSH PRIVATE KEY-----` | 高 |
| 带凭据的 URL | `postgres://user:pass@host`、`mysql://…` | 高 |
| 命令行/环境密码 | `--password=xxx`、`PGPASSWORD=xxx`、`MYSQL_PWD=xxx` | 高 |
| 账号 DDL | `IDENTIFIED BY '…'`、`SET PASSWORD = '…'`、`ALTER ROLE … PASSWORD '…'` | 高 |
| 已知密钥前缀 | `AKIA…`、`ghp_…`、`sk-…`、`xoxb-…`、`AIza…` | 高 |
| 赋值型字段 | `password = '…'`、`pwd:`、`secret=`、`token=`、`api_key=`、`access_key=` | 中 |
| INSERT 敏感列 | `INSERT INTO users(password, …) VALUES ('…')` | 中 |

### 9.3 降噪规则（关键）

- 忽略占位符与变量：`***`、`******`、`${…}`、`{{…}}`、`<password>`、`your_password`、
  `changeme`、`example`、`test`、`REPLACE_ME`、空串、单字符、全相同字符。
- 必须带可识别的值形态：引号字符串、URL userinfo、`=`/`:` 后的字面量；
  只有列名/关键字（如 `password` 单独出现）不报。
- 高置信规则不看上下文；中置信规则只在赋值/INSERT 形态下报，避免 `-- password 字段说明`
  这类注释噪音。
- 行内注释也扫描，但只跑高置信规则。
- 设置页可关闭“中置信规则”（默认开）；高置信不可关。
- 规则表放在 `app/secretscan.go`，用一组正/负样例测试，新增规则必须同时补反例。

### 9.4 交互

- 弹窗列出命中：文件、行号、规则、置信度、**脱敏预览**（如 `password = 'ab***yz'`）。
- 底部只有「取消提交」与「仍然提交」；后者是显式二次确认，默认焦点在取消。
- 超过 20 条时折叠为“其余 N 条”，完整清单可复制（同样是脱敏后的）。
- 取消后状态行提示“已阻止提交：发现 N 处疑似明文凭据”。
- 日志只记数量、规则名、文件路径与行号，**不记命中值**。

## 10. 冲突策略

一期（P1）安全中止：

- merge 冲突 → `git merge --abort`；返回 `ConflictItem{Path, Kind}` 列表；
- 本地 `connections.json` 与 `consoles/` 完全不动；
- 设置页显示「有 N 个文件冲突，请先处理后再拉取」，并附 `git status` 路径；
- 优点：绝不导入半成品；缺点：同一条控制台两边改过就同步不了。

二期（P2）按实体取舍（对齐 db-k）：

- `git diff --name-only --diff-filter=U` 只认 `db-g/` 下路径；
- 每个冲突项给 `:2:`（ours）/`:3:`（theirs）两份内容：
  - `.sql`：行数与差异片段；
  - `.enc`：解密后显示 name/kind/host:port/db/user，密码恒为 `****`；
  - JSON：显示实体名；
- 用户逐项选「本地/远端」，写回 + `git add` + 解决提交；
- 未全部选择时不能继续；取消 = 整次拉取放弃（`merge --abort`）；
- 解决后可自动 push（可选开关）。

## 11. 设置页与交互

设置窗口新增分区「同步」（`settingsSections()` 插在「连接档案」之后）：

```
仓库地址     [ git@github.com:me/db-g-sync.git ]   分支 [ main ]
SSH 私钥     [ ~/.ssh/id_ed25519            ] [选择…]
             留空则使用系统 ssh 配置 / ssh-agent
同步目录     <config>/db-g/sync/repo        （只读）
同步密钥     ● 已配置   [复制] [导入] [生成] [清除]
             仓库里的连接信息用它加密；丢失后无法恢复

[拉取]  [提交并推送]        状态：上次同步 11-14 15:02 · 成功
提示：控制台 SQL 以明文进入仓库；请使用私有仓库，不要把密码写进 SQL。
```

行为：

- 未配置仓库地址或密钥未设置：拉取/提交禁用并给原因。
- 运行中：按钮禁用 + 行内进度；不弹模态，结果写回状态行。
- 失败显示分类：git 缺失 / SSH 认证失败 / host key 未信任 / 非快进 / 冲突 / 密钥不匹配 / 仓库损坏。
- 密钥弹窗：单输入框（或“选择密钥文件”），`Enter` 提交；
  仓库已有 `key.kcv` 且本地无密钥时只提供“导入已有密钥”，不提供“生成新密钥”。

## 12. 安全与日志

- 仓库内不含连接明文；`*.enc` 是 AES-GCM 密文（文件名只有 id）。
- KCV 是明文校验值，不参与加密；它只用于快速发现错钥，安全性仍由 GCM tag 保证。
- 控制台 SQL 明文是显式取舍：README 与设置页都要提示；提交前用 §9 的规则拦截疑似明文凭据。
- 日志（tag `sync`）：只记 repo 路径、分支、实体计数、git 退出码/错误摘要、耗时；
  **不记** `sync.key`、密码、连接明文 JSON、扫描命中的原始值、完整 git 输出中的凭据。
- commit message 只含 `db-g sync: <hostname> <时间>`，不含实体名。
- `sync.key`、`sync.json` 均 `0600`；不进入日志、不进入仓库。
- 测试绝不写用户真实配置目录：仓库/配置/密钥一律 `t.TempDir()`。

## 13. 测试计划

| 层 | 用例 |
|---|---|
| `sync/secret` | 密钥生成/读写 0600、加解密往返、错误密钥失败、KCV 计算/比对、密文前缀 |
| `sync/paths` | 实体路径 ↔ id、非 `db-g/` 路径忽略、JSON/ENC 扩展名 |
| `sync/engine`（临时裸仓，git 缺失则 skip） | init→commit→push；空仓库首次拉取只初始化并绑定分支；clone→import；两边改不同控制台自动合并；两边改同一控制台冲突检测；一边删一边没改传播删除；空库守卫；push 被拒 |
| `sync` 去抖 | 明文相同不重写 `.enc`；改一个字段才产生文件变化 |
| `app` 导入 | 解密失败整体不写；孤儿控制台跳过；外部控制台保留；caret/active 保留；删除传播；单次落盘 |
| `app/secretscan` | 高置信正例全部命中；占位符/变量/列名不误报；中置信正反例；指纹去重；脱敏预览 |
| `ui` headless | 设置页字段/密钥状态；无密钥时弹窗；仓库有 check 时禁止生成；按钮禁用原因；同步成功后刷新树与编辑器 |
| 人工 | 两台设备：A 配连接+控制台 → 提交；B 拉取 → 连接可用、SQL 一致；改不同实体互拉无冲突；改同一 SQL 走冲突流程 |

## 14. 风险与待定

| 风险 | 影响 | 对策 |
|---|---|---|
| 丢失 `sync.key` | 仓库连接永久无法解密 | 生成时强提示 + 复制/导入；不提供跳过密钥后门；README 说明 |
| 错钥设备把解出的脏数据推回仓库 | 仓库被污染 | KCV 在解密/commit 前比对；不一致直接中止；GCM tag 二次兜底 |
| 敏感 SQL 误报太多 | 用户关掉扫描或无脑点确认 | 高置信才阻断、中置信可关；fingerprint 去重；占位符/变量降噪；一次弹窗 |
| 用户以为控制台 SQL 也加密 | 敏感 SQL 进远端 | 设置页与 README 双提示；后续可加“SQL 也加密”开关（会失去文本合并） |
| 裸 pull 覆盖本地改动 | 数据丢失 | 强制「先导出本地快照并提交」；首次同步守卫 |
| 冲突处理缺位（一期） | 双设备改同一 SQL 无法同步 | 一期明确中止且不写本地；二期补按实体取舍 |
| SSH 私钥有口令且无 agent | push/pull 失败 | `BatchMode` 快速失败 + 明确文案；推荐部署密钥/agent |
| 连接重名 | 无法靠名称区分 | 全程按 id；树里重名是现有表现 |
| 大 SQL 让仓库膨胀 | clone 变慢 | 控制台按文件存、git 压缩；不做自动历史清理 |
| 远端仓库被共享/公开 | SQL 泄漏 | 文档强制建议私有仓库；不写连接明文 |

待定：

- 是否把 `keymap.json`/`theme.json`/`ai.json` 纳入同步（本期按用户范围排除）。
- 是否需要“拉取后自动推送”（默认关，避免把刚合并的仓库无确认推出去）。
- 是否需要同步远程控制台正文的 `updatedAt` 用于排序（当前 `ConnStore` 已有 `UpdatedAt`，直接同步）。
- 密钥轮换：首版只支持导入/清除；轮换需重加密全部 `.enc` 并提交，留到二期。

## 15. 实施切片

1. **P1 引擎**：`sync/`（secret/git/paths/models/engine）+ `sync.json`/`sync.key` + 单元测试；
   不动 UI。
2. **P2 适配与导入**：`app/syncadapter.go`（Snapshot/ApplySync、单次落盘、外部控制台保留）+
   拉取/提交完整流程 + KCV 校验 + 空库守卫 + `app/secretscan.go` 与确认弹窗 + 测试。
3. **P3 UI**：设置页「同步」分区、密钥弹窗、拉取/提交按钮与状态、同步后刷新；
   headless 测试 + 人工两台设备验收。
4. **P4 冲突取舍**：按实体弹窗/自动推送开关。

规模判断 [INFERENCE]：P1–P3 约 1200–1600 行含测试；P4 约 400–600 行。
