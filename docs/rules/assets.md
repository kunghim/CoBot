# 资源规则

触发：改 `assets/**`。日常构建会自动处理这些资源，不改资源时不需要手动运行下面的命令。

- 头像：改 `assets/avatars` 的源文件后运行 `node assets/avatars/build.mjs` 重新生成，再用 `--check` 校验。
  系列文件带版本、大小和校验值，不手改生成结果。
- 品牌图标：遵循 [assets/branding/README.md](../../assets/branding/README.md)，复用已提交的金属鸟标识，不换形、不重新着色。
  改完运行 `node scripts/prepare-branding.mjs` 校验（构建脚本和 CI 也会调用）。
- 新增素材写明来源和许可证，同步 [依赖与第三方声明](../reference/dependencies.md)。

做完：`node assets/avatars/build.mjs --check` 和 `node scripts/prepare-branding.mjs` 通过。
