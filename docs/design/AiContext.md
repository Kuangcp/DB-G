> 已定稿的技术设计与实施计划：
> `docs/superpowers/specs/2026-11-09-ai-context-design.md`、
> `docs/superpowers/plans/2026-11-09-ai-context.md`。本文件保留产品愿景与场景描述。

为了在架构初期就将 AI 补全、执行计划可视化、本地聊天流 作为一等公民（First-class citizen）融入 DB-G，我们必须彻底颠覆传统“输入框 + 结果集”的被动设计，将 AI 视为一个拥有完整上下文感知、具备工具调用能力（Function Calling）的内核协程。
以下是实现这一愿景的系统级架构设计、AI 的操作边界、以及核心应用场景蓝图：

一、 核心架构：AI 可以读取和操控什么？

在数据库客户端中，AI 的强大取决于你喂给它多少上下文（Context）。在 DB-G 的 Go 架构中，应设计一个统一的 AIContextManager，允许 AI 在严格的隐私和安全边界内读取和操控以下三个维度的内容：

1. AI 可读取的静态与动态内容

• 大库元数据（Schema Context）： 不仅是表名、列名、数据类型，还包括主外键关系、索引设计、触发器、约束（Check），甚至表与列的注释（Comment）。这是 AI 理解业务逻辑的基石。
• 运行时状态（Runtime Context）： 当前激活的数据源品牌（MySQL 还是 ClickHouse，因为 SQL 语法不同）、当前会话的 Session ID、当前正在使用的 Schema/Database。
• 用户意图与历史（Behavioral Context）： 编辑器当前选中的 SQL、光标所在的位置、当前工作区打开的所有控制台正文（跨文件上下文）、以及最近执行失败的错误日志（Error Log）。
• 数据采样（Data Profiling, 可选且需安全脱敏）： 允许 AI 对特定表执行 LIMIT 5 的流式采样，让 AI 看到真实的样本数据（例如知道 status 列里存的是 "INIT"、"SUCCESS" 还是 "FAILED"），从而生成更精准的条件条件句。

2. AI 可操控的动作（Action API）

不能让 AI 直接盲目执行写操作，但 AI 应该拥有操纵 UI 和流（Stream）的最高权限：
• 操纵编辑器： 在光标处插入/替换代码、自动补全当前行、将生成的 SQL 高亮展示并等待用户按 Tab 确认。
• 操纵控制台： 自动创建新的临时控制台标签、自动将长 SQL 进行格式化排版。
• 操纵执行计划流： 拦截用户的查询，自动在前面加上 EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)，并将结果转化为内部的图结构。

二、 AI 如何与日常开发深度结合（核心场景）

除了“对话式让 AI 生成复杂统计报表 SQL”之外，AI 作为一等公民还可以极大地重构以下高频日常开发场景：

场景 1：精准的“跨表 & 关联关系”大模型自动补全 (Copilot-like Auto-complete)

• 传统补全痛点： 打出 SELECT * FROM users u JOIN  之后，传统补全只能盲目提示所有表名。
• AI 一等公民设计： 补全引擎在后台实时维护一个轻量级的 Vector 缓存或图结构（包含 schema 和主外键）。当你打出 JOIN 时，AI 补全根据外键关系、历史查询习惯，直接预测出 user_profiles p ON u.id = p.user_id，用户只需按一下 Tab 键。甚至在输入 WHERE 时，AI 能根据列名 created_at 自动补全常用的时间段函数。

场景 2：一键执行计划“大白话”诊断与可视化 (Explain Analyzer)

• 开发痛点： 传统的 EXPLAIN 输出是几百行的树状文本或 JSON，开发者极难一眼看出为什么慢（是全表扫描？还是隐式类型转换导致索引失效？）。
• AI 一等公民设计：
	1. 用户在结果区或编辑器点击 “AI 诊断执行计划”。
	2. DB-G 后台自动运行 EXPLAIN，将执行计划 JSON 和表结构的索引信息打包作为上下文喂给 AI。
	3. UI 不仅用红黄色块可视化展示代价（Cost）最高的节点（如 Seq Scan），AI 还会直接在侧边栏输出大白话诊断结论与优化建议：💡 AI 诊断报告：
		• 慢因： orders 表的 user_id 正在进行全表扫描，耗时占比 85%。
		• 原因： 该列缺少索引，且查询中发生了 VARCHAR 与 INT 的隐式类型转换。
		• 一键优化： [点击执行] CREATE INDEX idx_orders_user_id ON orders(user_id);

场景 3：本地聊天流（Chat-to-SQL）生成复杂报表

这就是你提到的场景，但要做到极致，必须解决元数据过大（Token 溢出）和多库语法差异的问题。
• 实现机制（RAG / 向量检索）：
	1. 局部上下文组装： 用户输入：“帮我统计上个月每个省份消费总额前三的 VIP 用户”。
	2. DB-G 内部的 AI 引擎不会把整库 6 万张表丢给大模型。它首先通过本地关键词检索/Embedding，找出最相关的几张表（如 users, orders, regions）的 DDL。
	3. 针对性 Prompt 构建： 结合当前方言（如 PostgreSQL 15），构建 Prompt，要求 AI 必须使用窗口函数（DENSE_RANK()）来实现“前三名”。
	4. 流式输入与预览： AI 在新标签页中流式（Streaming）打出 SQL，并在关键的聚合、JOIN 条件旁自动加上 Markdown 注释。

场景 4：生产环境“高危操作”主动拦截与安全审计 (Guardrail)

• 开发痛点： 手抖删库、忘记写 WHERE 条件导致全表更新，是开发者的噩梦。
• AI 一等公民设计：
AI 作为一个异步审计拦截器（Middleware）。当用户按下 Ctrl+Enter 执行语句时：
	• 如果检测到 UPDATE / DELETE 语句完全没有 WHERE 条件，或者 WHERE 1=1，立即弹窗拦截并红字警告。
	• 如果检测到影响行数可能巨大的操作，AI 结合数据库的元数据计数，提示：“警告：该操作预计将影响 logs 表的 500 万行数据，且当前未开启显式事务（AUTOCOMMIT=ON），是否自动包裹在 BEGIN...ROLLBACK 事务中执行？”

场景 5：线上报错（SQL Error）一键“自动修复” (Auto-Fix)

• 开发痛点： 面对不同数据库稀奇古怪的报错代码（如 MySQL 的 Error 1064，PostgreSQL 的 relative operator does not exist），开发者往往需要去 Google 或 StackOverflow 搜半天。
• AI 一等公民设计：
当控制台结果区抛出错误横幅时，紧跟一个 “✨ AI 一键修复” 按钮。AI 读取当前执行失败的 SQL + 驱动返回的精确错误堆栈 + 目标表的列定义，直接在编辑器里高亮标出写错的语法或拼错的列名，并提供修改后的 Diff 对比视图，用户点击即可覆盖并重新运行。

三、 💡 给 DB-G 初期架构设计的落地建议

如果你要在代码级别将这些列为“一等公民”，在底层的 app/ 和 engine/ 目录设计中，应该做以下两件事：
1. 统一的 Context 协议（Connector 接口扩展）：
让驱动层（SQLite, MySQL, PG）不仅要返回数据，还要提供一个 GetSchemaSnapshot() 方法，能够高效、增量地将库表结构转化为 AI 容易理解的 Markdown 或 JSON 摘要。
2. 本地/远程 AI 驱动解耦（ai/ 包设计）：
设计一个统一的 AIEngine 接口。
	• 远程模式： 支持用户配置自己的 OpenAI / Anthropic / DeepSeek API Key。
	• 本地隐私模式（杀手级特性）： 鉴于很多企业禁止将数据库元数据上传到公网，DB-G 作为纯 Go 项目，可以通过 cgo 释放或通过 RPC 调用本地的 Ollama（运行 deepseek-coder 或 llama3）。这样所有表结构和查询历史完全保留在开发者本地电脑上，极具商业说服力。
针对上述场景，我们可以进一步落实细节：
• 你更倾向于让这个 AI 助手使用 完全本地化的 LLM（如本地 Ollama 联动），还是通过 公网 API（如 DeepSeek、OpenAI） 来驱动？
• 对于 场景 4（高危操作拦截），你认为应该做成强制性的硬阻断，还是只做人性化的弱提示？
