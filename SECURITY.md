# Security Policy

## Supported Versions

| Version | Supported |
| --- | --- |
| Latest release | ✅ |
| Older releases | ❌ |

仅最新正式发行版受支持。如有安全漏洞需要回退到旧版本，请在报告中说明。

## Reporting a Vulnerability

请通过 GitHub [Issues](https://github.com/Kuangcp/DB-G/issues) 提交安全问题报告，**标题以 `[SECURITY]` 开头**。如果是私人仓库 fork，请直接邮件联系作者（README 上的联系方式）。

报告需包含：

- 漏洞描述与复现步骤
- 受影响的功能模块（连接加密、SQL 注入防护、对象树渲染等）
- 你运行的版本和 OS 环境

我会在 **48 小时内**确认收到并给出初步评估。如需，我会邀请你加入私密 Issue thread 进行进一步交流。

## Scope

以下属于本报告范围：

- 密码存储与传输中的明文泄漏（`secret.key` 权限不足、日志中出现密码原文）
- SQL 执行层的注入风险（多语句解析、参数化拼接）
- UI 渲染导致的脚本注入（DDL 预览、JSON 查看器输出为可执行代码）
- 文件读写越权（外部 `.sql` 拖拽加载的路径穿越、配置目录权限问题）
- 第三方依赖的已知高危 CVE（clickhouse-go / pgx / mysql driver / modernc.org/sqlite）

以下不在本报告范围：

- mygo 框架自身的缺陷（属于上游治理范围）
- 操作系统层面的本地提权攻击
- 社会工程 / Phishing

## Existing Security Measures

项目已内置的安全机制：

- **密码加密** — 连接密码使用 bcrypt 加密后存入 `connections.json`；密钥 `secret.key` 权限固定为 `0600`。存量明文连接档案在首次加载时自动加密回写，密钥变更导致解不开时会提示重填。
- **日志脱敏** — `app.RedactDSN()` 自动过滤密码字段；所有日志均经脱敏处理，DSN 中不出现 `Password` 键值。
- **无遥测** — 不收集使用数据、不上报崩溃指标、不联网校验更新。
- **纯静态二进制** — 无 Electron/WebView，不渲染不受控的 HTML，降低 web 攻击面。
- **SQL 格式化保守策略** — 只改空白与关键字大小写，不做语义重排，防止格式化引入 SQL 变更。

## Disclosure Process

1. 收到报告后 48h 内确认。
2. 评估严重程度（CVSS 参考）。
3. 修复并测试。
4. 发布补丁版本并公开公告。
5. 致谢贡献者（除非选择匿名）。

### Severity Guide

| 级别 | 描述 |
|---|---|
| Critical | 可直接窃取密码或执行任意代码 |
| High | 可导致未授权访问或数据泄露 |
| Medium | 可通过精心构造的输入触发意外行为 |
| Low | 信息泄漏面较小、利用难度高 |
