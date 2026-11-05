# DB-G

对标 db-k 的数据库客户端（Go + mygo），规格与里程碑见
`docs/superpowers/specs/2026-10-14-db-g-roadmap-design.md`。

## 功能清单

- [x] 编辑器：mygo `ui/editor`（SQL 高亮、行号、查找替换 Ctrl+F、折叠、多光标、撤销；
      百万行级视口布局，性能基准见 docs/spike-results.md 的 A1 一节）
- [x] M1：打开 SQLite 文件、异步执行 SQL（Ctrl+Enter）、虚拟化结果表格
      （行号/截断徽章/错误横幅/停止取消/500 行与 1MB/16MB 预算）
- [x] M2：主页面布局对齐 db-k 骨架（侧栏/顶栏/单行工具栏）；连接管理
      （JSON 持久化/新建/编辑/删除/状态点）；SQLite 对象树（懒加载）
- [x] 选区执行：仅执行编辑器选中的 SQL，无选区时禁用（禁止跑整个控制台）
- [x] 数据源：SQLite / MySQL / **PostgreSQL**（pgx）/ **ClickHouse**（只读优先）
      —— 连接、执行、对象树、列目录（含主键）、补全、导出；PostgreSQL 支持写操作，
      ClickHouse 只读；多 schema 切换与 PG 的 DDL 查看待做（见 P3 记录）
- [x] 控制台 + 工作区：一个数据源多个命名控制台，每个绑定
      `~/.config/db-g/consoles/<id>.sql`；标签条/工作区切换、防抖自动落盘、
      Ctrl+S 立即保存、重启恢复工作区与控制台正文/光标。**关闭标签（×）只是移出当前
      工作区，文件保留**；右键「删除…」才删元数据与 .sql。侧栏每个数据源下列出它的
      全部控制台，可重新打开。
- [x] P1 日常查询闭环：
      **多语句执行**（有选区跑选区、无选区跑光标语句；逐条顺序执行、失败即停、
      结果按语句分 Tab、编辑器行底色标记语句状态）；
      **结果区**：单元格/整行复制、右键「复制本行 → INSERT」、长文本/JSON 查看器、
      右键表头排序（NULL 总在最后）与每列筛选、**取更多**（派生表分页追加）、
      **导出** CSV / JSON / SQL INSERT（含全量流式导出，重新执行查询）；
      **SQL 历史**（`history.jsonl`，5MB 轮转，面板可插入光标/复制）；
      **对象树**双击预览数据、右键查看 DDL（`Ctrl+Q` 待补）；
      **保守 SQL 格式化**（只改空白与关键字大小写，可撤销）
- [x] M3（部分）：多语句执行 + SQL 历史（语句级进度/gutter 状态已做，历史面板已做）
- [x] P2 写操作（M6）：单表 + 主键的结果可编辑 —— 弹窗改格、增行、删行、撤销；
      提交前逐条参数化 SQL 预览，**单事务**执行，影响行数 ≠ 1 整体回滚；
      JOIN / 视图 / 主键不在结果里 / 别名表达式列一律只读（工具条显示原因）；
      写操作需要驱动声明 `CapEditableResult` + 实现 `Transactional`（sqlite / mysql 均已实现）
- [x] 自动补全（A2）：SQL 关键字/函数/类型 + 当前数据源的表/视图；打字自动弹，
      `Ctrl+Space` 显式触发；表/视图目录按连接缓存（TTL 5 分钟，顶栏「设置」里
      可手动「刷新补全缓存」）；列补全按**光标所在语句/子查询引用的表**提示
      （FROM · JOIN · UPDATE · INTO，支持多表 JOIN、别名与 CTE 展开；语句里没有表时不出列）
- [x] 日志/诊断（P0）：`~/.config/db-g/logs/app-YYYYMMDD.log`（JSON Lines，一天一文件、
      5MB 轮转，`0600`）；记录启动/连接/执行（耗时·行数·完整 SQL）/控制台/补全加载失败/panic
      堆栈；设置窗口可打开日志目录。DSN 与密码字段自动脱敏。
- [ ] M7：编辑器格式化（补全已完成：关键字/表/列）

## 构建与测试

```bash
export GO=/home/zk/Code/go/bin/go1.27.3
CGO_ENABLED=0 $GO run .     # 交互窗口（本机 cgo 链接有 .sframe 环境问题，统一关掉 cgo）
CGO_ENABLED=0 $GO test -p 1 ./... # 全量（含编辑器与结果网格基准；-p 1 避免并行包争 CPU 导致基准超限）
CGO_ENABLED=0 $GO test ./ui -run 'TestEditor.*Perf' -v   # 只跑编辑器性能基准
$GO tool mygo vet .         # UI 生命周期检查
```

顶栏「设置」打开独立设置窗口（单例）：编辑器信息（行数/字节/版本）与 3k/12k/100k
演示预设（写入当前激活控制台并立即落盘，性能自测用）。
