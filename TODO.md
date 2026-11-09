> 重要： 当前记录的是待办事项，如果完成实现或修复并且通过验收就删掉对应项（除此之外，模型不要主动改这个文件的内容）。

# 编辑区域


## 自动补全
- 补全作用域残留：上一条语句漏写分号、光标在下一条语句的 SELECT 列表 / WHERE 等位置时，
  前一条语句的 FROM 表仍可能进入列补全候选。
    - 复现：`select * from b\nselect id, na| from med_school`（光标在 `na` 后），
      候选里可能混入 `b` 的列；当前语句自己的列也可能被前一条语句带偏。
    - 已做：FROM/JOIN/UPDATE/INTO 的关系名位置优先给表（方案 A）；
      光标之后的行首 `SELECT`/`INSERT` 等新语句已隔离（方案 B）。
    - 未做：光标之前缺分号的语句做软切分（方案 C，需处理 UNION / CTE / INSERT…SELECT 误切）。
    - 相关代码：`ui/sqlscope.go` 的 `stmtScopeAtOffset` / `statementBounds` /
      `softStatementEnd` / `relationPositionAt`；测试 `TestStmtScopeAtSoftBoundaryGuards`、
      `TestCompletionRelationPositionWithPreviousStatement`。
- AI 助手后续阶段（Chat-to-SQL、EXPLAIN 可视化 + AI 诊断、SQL 错误修复、高危 SQL 拦截、
  内联 AI 补全已完成，设计与计划见 `docs/superpowers/specs/2026-11-09-ai-context-design.md`、
  `docs/superpowers/plans/2026-11-09-ai-context.md`）：
    - 向量/embedding 检索（当前仅词法 + 外键扩展）

- Ai设置里 先填地址和key，最后选模型，填完前面俩就可以通过 api协议里的/models 接口获取到模型列表了，下拉选择，如果获取不到就用户手动输入模型名


# 查询结果
- 添加一行  需要修复和重新设计
- ClickHouse 作为一个大宽表分析型数据库，用户最核心的诉求通常不是 DDL 查看或简单的只读，而是大数据量的高效流式导出（TSV/Parquet）、复杂的聚合查询可视化（执行计划/火焰图分析）。
- Explain 的各数据源的渲染支持
- 复制出SQL insert 一样有转义的问题，看db-k的修复记录 那边修复过这个问题，就是把json字段内字符串的内容转义处理，保持JSON结构正确，复制出insert，再执行，让数据是一致的。

# 左侧树区域

# 驱动加载问题

之前遇到ck的版本兼容问题，最后是降级解决的，但是以后如果需要同时连 非常低版本的pg，以及非常高版本的pg 怎么区分不同驱动来连不同数据源呢


# 杂项

• 项目引入了 24h TTL 的本地 JSON 缓存来加速对象树展开。
• 缺陷：24h 的过期时间对于频繁变更结构（如正在进行发布、加字段、建索引）的开发环境来说太长了。虽然提供了“手动重载”，但如果用户依赖自动补全（补全缓存 TTL 5分钟），而表结构已经发生变化，会导致生成的 SQL 频繁报错。元数据缺少一种轻量级的“检测变更（如检测 PostgreSQL 的系统表修改时间）”的主动感知机制。

- 升级检测
    - 检测Github的 https://github.com/Kuangcp/DB-G release吧。如果有新发布的release 就下载下来（本地version文件对比）

- 打包
    - Linux 自动生成 desktop 到 ~/.local/share/applications 图标到 ~/.local/share/icons 下。
        - AppImage 可以通过 AppImageLauncher 完成
        - fpm (Effing Package Management) 手动制作 deb rpm
    - Windows
        - Inno Setup

- 拖入csv 作为数据源，使用内置的duckdb 转接进行写SQL做数据分析
