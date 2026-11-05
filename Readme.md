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
- [x] M4（部分）：MySQL 连接 + 执行 + 对象树（information_schema；PostgreSQL 未做）
- [x] 控制台 + 工作区：一个数据源多个命名控制台，每个绑定
      `~/.config/db-g/consoles/<id>.sql`；标签条/工作区切换、防抖自动落盘、
      Ctrl+S 立即保存、重启恢复工作区与控制台正文/光标。**关闭标签（×）只是移出当前
      工作区，文件保留**；右键「删除…」才删元数据与 .sql。侧栏每个数据源下列出它的
      全部控制台，可重新打开。
- [ ] M3：多语句控制台 + 执行历史
- [ ] M5：结果排序/筛选/分页/导出
- [ ] M6：结果编辑（事务写回）
- [ ] M7：编辑器补全与格式化

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
