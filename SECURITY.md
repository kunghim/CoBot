# Security policy / 安全政策

## Reporting a vulnerability

Do not report exploitable vulnerabilities in public issues or pull requests.
When private vulnerability reporting is available, use
[Report a vulnerability](https://github.com/kunghim/CoBot/security/advisories/new).
If that option is unavailable, open an issue asking the maintainer for a
private contact channel, without describing the vulnerability or posting
proof-of-concept code, credentials, private data or recordings. Wait for a
private channel before sharing details.

Include the affected version or commit, platform, reproduction, impact and
any suggested fix in the private report. There is no guaranteed response
time. The maintainer will coordinate investigation and disclosure.

## Supported versions

CoBot is an early preview. Security fixes target the current development
version and latest preview release, when one exists. Older previews do not
have a separate maintenance commitment. Desktop and macOS verification
limits are listed in the README; a successful build is not runtime proof.

## Data handling

CoBot stores application data locally and binds its service to loopback.
External Codex execution still sends relevant context to its configured
service; local storage does not mean that model requests stay offline.

Do not attach data roots, databases, engine homes, bootstrap links, cookies
or raw `cobot trace` recordings to public reports. Tracing is an opt-in
developer tool that records raw, unredacted traffic, potentially including
credentials and private content. Share a minimal, sanitized reproduction.
If a credential is exposed, revoke or rotate it; deleting an attachment or
commit alone does not make the credential safe.

## 中文说明

可利用的安全漏洞不要发到公开 Issue 或 PR。私密漏洞报告启用时，请使用上面的
“Report a vulnerability”入口；如果不可用，只提交“申请私密联系渠道”的 Issue，
不要包含漏洞细节、复现代码、凭据或私人资料，待维护者提供私密渠道后再说明。

私密报告请提供版本或 commit、平台、复现步骤、影响和建议修复方式。项目处于早期
预览阶段，安全修复面向当前开发版本和最新预览版，不承诺固定响应时间或旧版本维护。

本地保存数据不等于模型调用离线：Codex 仍会将相关上下文发送到其配置的服务。
不要上传数据根、数据库、引擎目录、启动链接、Cookie 或原始 trace。trace 会原样
记录流量，可能包含凭据和私人内容；普通反馈只需提供最小、人工脱敏的复现资料。
已泄露的凭据应撤销或轮换，仅删除文件或提交无法使凭据重新安全。
