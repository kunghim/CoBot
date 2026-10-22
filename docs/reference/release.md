# 发布草稿

适用场景：维护者把 `main` 上已测试的提交打包成发布草稿并公开发布。

## 触发

1. `dev` 晋级到 `main` 后，打开 **Actions → release draft → Run workflow**，选择 `main`。
2. 填写产品版本（不带 `v`，如 `0.1.0-beta.2`）和单独的数字 MSI 版本（如 `0.1.2`）。
   MSI 版本必须大于最近一次已发布的 Windows MSI；工作流会下载那个安装包读取 ProductVersion，但不安装。

## 工作流做什么

- 对选定的提交重新跑一遍 CI，然后在 Windows 上构建 Windows x64，在 Apple Silicon 上构建 macOS ARM64。
- 打包工具版本锁定在 `.github/workflows/release.yml` 中，以该文件为准。
- 检查 MSI 内容、比对 Windows ZIP 与 MSI 载荷、挂载并复制 DMG、检查两个 macOS 可执行文件和复制后的临时签名、校验所有上传文件的校验和。
- 生成未公开的草稿，恰好包含三个应用下载：Windows x64 MSI 和 ZIP、Apple Silicon DMG，各带一个 SHA-256 文件；
  另附 `release-manifest.json`，记录源提交、构建运行、产品与 MSI 版本、文件大小和哈希。
- Intel 包、服务端二进制、原始构建日志和运行配置不作为发布资产；本地 macOS 构建可能产出 ZIP，但工作流只上传 DMG。

## 安全边界

- 只有最后的草稿任务拥有仓库内容写权限。推分支、推标签、合并 PR 都不会发布版本。
- 已存在的版本和标签会被拒绝，不会覆盖。上传失败留下不完整草稿时，先检查再重试；工作流不会覆盖它。不要发布资产不全的草稿。
- 签名凭据和 Codex 登录不进入工作流。

## 发布前验收

1. 下载草稿里的同一批资产。
2. 记录原生安装、首次启动，以及与本次改动相应的真实 Codex 检查。
3. 在发布说明中写明限制：Windows 包未签名；macOS 只有临时（ad-hoc）签名，没有 Developer ID 签名和公证；
   自动打包不代表真实账号或交互式安装已验收。
4. 补充实际验收证据后，由维护者发布草稿。
