# 依赖与第三方声明

- 核对日期：2026-10-20。
- 对照代码：`6b7530c`。版本以当前 `go.mod`、`go.sum` 和 `frontend/package-lock.json` 为准。
- 本文是依赖范围和直接依赖的维护说明，不是完整、机器可读的 SBOM。
- CoBot 自身采用 [Apache-2.0](../LICENSE)。

## 维护依据

Go 直接依赖表依据 `go.mod` 和对应版本的模块 LICENSE 核对；前端表中的确切版本来自锁文件，
并核对了匹配版本的包内许可证。旧验收中的版本号、当次审计结果不能替代当前锁文件。

完整发行包的第三方正文由 [third-party-notices.mjs](../scripts/third-party-notices.mjs)
在目标平台、安装锁定依赖后生成 `THIRD-PARTY-NOTICES.txt`。
该脚本按桌面构建标签收集程序、助手及传递模块，并包含 Go 标准库/运行时声明；Windows 另收集 WiX UI 资源声明。
前端遍历锁文件中的生产依赖，而非只收集第一层包。
脚本发现缺失许可证时会失败，不能通过删减名单跳过。

## Go 直接依赖

| 模块 | 版本 | 包内声明 |
| --- | --- | --- |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/modelcontextprotocol/go-sdk` | v1.8.0 | MIT 向 Apache-2.0 迁移；新代码 Apache-2.0，尚未完成再许可的贡献保留 MIT，以包内 LICENSE 为准 |
| `github.com/oapi-codegen/runtime` | v1.0.0 | Apache-2.0 |
| `github.com/pressly/goose/v3` | v3.24.1 | MIT |
| `github.com/wailsapp/wails/v2` | v2.15.0 | MIT |
| `golang.org/x/sys` | v0.46.0 | BSD-3-Clause |
| `modernc.org/sqlite` | v1.46.2 | BSD-3-Clause |

此表列出直接模块，不能据此推断某个 server 或 desktop 产物包含全部这些模块。
实际编译闭包包含传递依赖，且随目标平台和构建标签变化；以该次构建生成的声明为准。
Go 标准库与运行时会进入二进制，不能因 Go 工具链是构建工具而遗漏其声明。

## 前端直接运行时依赖

| 包 | 锁定版本 | 包内声明 |
| --- | --- | --- |
| `@dicebear/core` | 10.6.1 | MIT |
| `@tanstack/react-query` | 5.104.2 | MIT |
| `i18next` | 24.2.3 | MIT |
| `lucide-react` | 0.475.0 | ISC |
| `react` / `react-dom` | 18.3.1 | MIT |
| `react-i18next` | 15.7.4 | MIT |
| `react-markdown` | 9.1.0 | MIT |
| `react-router-dom` | 7.18.4 | MIT |
| `remark-gfm` | 4.0.1 | MIT |

表格使用锁文件中的确切版本，不使用 `package.json` 的版本范围代替实际版本。
`react-markdown`、`remark-gfm` 等会引入传递依赖，完整清单由声明脚本收集。
Vite、TypeScript、Tailwind、Vitest、Playwright 和接口生成工具等开发依赖单独由各自锁文件维护。

## 头像素材与打包资源

头像渲染库和头像素材是两类来源：

- `@dicebear/core` 是前端代码依赖，见上表。
- 团子系列使用来自 `@dicebear/styles` 10.6.0 的 Gaze 素材，其声明为 CC0 1.0。
- CoBot 补充的形状、色板和角色默认值采用仓库许可证 Apache-2.0。
- 来源及许可证见 [Dango LICENSE](../assets/avatars/dango/LICENSE.md)。
  `dist/dango.json` 自带素材声明；头像系列由核心下载和缓存，不应被误记为内嵌的完整素材包。
- Windows MSI 中的 WiX UI 模板和资源另有随包声明，来源保存在
  [packaging/licenses/wix](../packaging/licenses/wix)。
- 应用图标、界面标识与 favicon 随程序打包，来源和导出方式见
  [品牌资源说明](../assets/branding/README.md)，不通过头像库下载。
  Python/Pillow 只用于重新导出品牌图像，日常构建复用已提交的图标，不要求安装 Python。

升级头像系列、替换素材或改变下载/内嵌方式时，要同时检查素材来源和声明，不能只更新 npm 依赖表。

## 外部运行条件

Codex CLI 由用户安装，CoBot 不将其放入发行包。当前兼容性下限按平台区分，见[开发指南](development.md)。
Windows 桌面依赖已有 WebView2 Runtime；macOS 原生构建依赖平台工具链。
这些外部条件与内嵌依赖分开说明，不将“需要用户安装”写成“由 CoBot 分发”。

## 如何更新

1. 按锁文件安装依赖，核对目标平台与构建标签。
2. 对照 `go.mod` 和前端锁文件更新直接依赖表，并读取匹配版本的 LICENSE。
3. 使用目标平台打包脚本生成第三方正文，检查遗漏；不能用旧包的声明替换新包。
4. 改动涉及头像等素材时，同步其来源和许可证文件。
5. 在 PR 中说明范围和实际检查结果，不将历史 `npm audit` 结果写成当前无风险保证。

在依赖齐备的目标平台可单独生成声明用于复核：

```sh
node scripts/third-party-notices.mjs .tmp/THIRD-PARTY-NOTICES.txt
```

本次仅核对源文件、锁文件与直接依赖许可证，未执行 Windows 或 macOS 原生打包。
