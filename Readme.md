# DB-G

对标 db-k 的数据库客户端（Go + mygo），规格与里程碑见
`docs/superpowers/specs/2026-10-14-db-g-roadmap-design.md`。

## 功能清单

- [x] 长文本编辑器：虚拟化渲染 + 增量 SQL 高亮 + 查找（性能基准见 docs/spike-results.md）
- [x] M1：打开 SQLite 文件、异步执行 SQL（Ctrl+Enter）、虚拟化结果表格
      （行号/截断徽章/错误横幅/停止取消/500 行与 1MB/16MB 预算）
- [ ] M2：连接管理 + 对象树
- [ ] M3：多语句控制台 + 执行历史
- [ ] M4：MySQL/PostgreSQL（方言与能力位抽象验证）
- [ ] M5：结果排序/筛选/分页/导出
- [ ] M6：结果编辑（事务写回）
- [ ] M7：编辑器补全与格式化

## 构建与测试

```bash
export GO=/home/zk/Code/go/bin/go1.27.1
$GO run .            # 交互窗口
$GO test ./...       # 全量（含 spike 与结果网格基准，数分钟）
$GO tool mygo vet .  # UI 生命周期检查
```
