<!-- English or Chinese is welcome. / 欢迎使用中文或英文。 -->
<!-- One PR is enough; no separate translation PR is needed. / 无需为翻译另发重复 PR。 -->

## Problem and related issue / 问题与关联 Issue

<!-- Explain the problem and trigger. Link an existing issue with Refs #123; small fixes do not require a new issue. -->
<!-- 说明问题与触发场景。已有 Issue 用 Refs #123 关联；小修复无需为此新建 Issue。 -->
<!-- Ordinary PRs target dev. Closing keywords belong in the dev -> main promotion for issues actually resolved there. -->
<!-- 普通 PR 向 dev 提交；实际解决的问题在 dev -> main 晋级 PR 中用关闭关键词处理。 -->

## Resulting behavior / 改动后的行为

<!-- Describe behavior before/after and the scope. Keep one logical change per PR; concise descriptions are fine for small fixes. -->
<!-- 描述改动前后的行为和范围。每个 PR 聚焦一项改动；小修复可以简写。 -->

## Validation / 验证

<!-- Commands, results and limitations. Separate automated, real Codex and native platform checks. -->
<!-- 写明命令、结果和未测范围，区分自动化、真实 Codex 和原生平台验证。 -->
<!-- Say why a check was skipped; do not mark an untested platform as verified. -->
<!-- 未运行的检查请说明原因，不要把未测平台标为通过。 -->

## Compatibility and data impact / 兼容性与数据影响

<!-- Write None if unaffected. Otherwise explain API/CLI/configuration changes, database migrations, upgrade/rollback effects, and required user actions. -->
<!-- 无影响请写“无”；否则说明 API/CLI/配置、数据库迁移、升级回滚影响及用户需要采取的措施。 -->
<!-- Flag actual incompatible changes with breaking-change (or ask a maintainer to label them), and include migration guidance. A migration or internal refactor alone is not automatically breaking. -->
<!-- 真正不兼容的改动请标 breaking-change（或请维护者标记）并提供迁移指引；普通迁移或内部重构不自动算破坏性变更。 -->

## Screenshots / 截图

<!-- For visible UI changes; remove this section otherwise. / 界面改动请附脱敏截图，其他改动可删除本节。 -->

## Review checklist / 检查

- [ ] The change is focused and relevant checks passed, or failures are explained. / 改动聚焦，相关检查通过或已说明失败。
- [ ] Compatibility, user data, upgrade and rollback effects are assessed above. / 已说明兼容性、用户数据、升级与回滚影响。
- [ ] Relevant UI text is available in English and Chinese. / 涉及的界面文案已覆盖中英文。
- [ ] No credentials, private AI instructions, runtime data or raw traces are included. / 未包含凭据、私人 AI 指令、运行数据或原始 trace。
- [ ] I have the right to submit this material under Apache-2.0 and preserved third-party attribution. / 有权按 Apache-2.0 提交并保留第三方声明。

<!-- Target dev. Only maintainers' dev -> main promotion PRs target main. -->
<!-- 普通 PR 向 dev 提交；main 只接受维护者从本仓库 dev 发起的晋级 PR。 -->
<!-- Use Draft while unfinished. CI passing alone does not authorize merging; maintainers decide acceptance and release timing. -->
<!-- 未完成时使用 Draft；CI 通过不代表获准合并，是否接受和何时发布由维护者决定。 -->
