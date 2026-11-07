> 重要： 当前记录的是待办事项，如果完成实现或修复并且通过验收就删掉对应项（除此之外，模型不要主动改这个文件的内容）。

# 编辑区域

参考DB-K的实现，可以从外部拖入一个SQL文件，然后弹出数据源选择后绑定，然后就可以当作是一个临时的控制台来执行和修改，但是这个时候因为有外部修改的风险所以需要考虑文件内容的一致性问题。

## 自动补全
- Ai设计接入 ： 架构初期就将 AI 补全（Copilot-like context）、执行计划可视化分析、本地聊天流（Chat-to-SQL） 作为一等公民（First-class citizen）来设计
    - 草稿设计在 AiContext.md


# 查询结果
- 添加一行  需要修复和重新设计
- ClickHouse 作为一个大宽表分析型数据库，用户最核心的诉求通常不是 DDL 查看或简单的只读，而是大数据量的高效流式导出（TSV/Parquet）、复杂的聚合查询可视化（执行计划/火焰图分析）。

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

