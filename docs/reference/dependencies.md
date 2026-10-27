# 依赖与第三方声明

- 核对日期：2026-10-20。
- 对照代码：`6b7530c`。版本以当前 `go.mod`、`go.sum` 和 `frontend/package-lock.json` 为准。
- 本文是依赖范围和直接依赖的维护说明，不是完整、机器可读的 SBOM。
- CoBot 自身采用 [Apache-2.0](../../LICENSE)。

## 维护依据

Go 直接依赖表依据 `go.mod` 和对应版本的模块 LICENSE 核对；前端表中的确切版本来自锁文件，
并核对了匹配版本的包内许可证。旧验收中的版本号、当次审计结果不能替代当前锁文件。

完整发行包的第三方正文由 [third-party-notices.mjs](../../scripts/third-party-notices.mjs)
在目标平台、安装锁定依赖并构建前端后生成 `THIRD-PARTY-NOTICES.txt`。
该脚本按桌面构建标签收集程序、助手及传递模块，并包含 Go 标准库/运行时声明；Windows 另收集 WiX UI 资源声明。
前端按构建生成的 `internal/webui/dist/third-party-inputs.json` 收集：覆盖主页面、Worker、DOCX 独立渲染器、CSS 输入、构建注入的运行时代码及明确复制的资源，包括实际使用的传递依赖和资源子目录声明。仅被 npm 安装、未进入产物的依赖不纳入发行声明，不使用包名豁免名单。
记录包含锁文件、构建输入和输出文件的哈希；缺失或过期、输出变化、已使用的包缺少许可证时均失败。正式打包还要求每个二进制嵌入同一份记录，拒绝用新构建的声明配旧二进制。
`fontkit`、`dfa`、`brotli` 属于 PPTX 预览库独立 Node 入口的依赖；当前 CoBot 使用浏览器入口，构建记录确认它们未进入产物。后续代码若引入这些包，会重新纳入检查。
`isarray` 1.0.0 的完整 MIT 正文位于包内 README 的 License 小节，按精确版本和正文特征提取。
`@napi-rs/canvas` 1.0.13 的平台包漏带正文，使用经同版本上游比对的[原文与来源记录](../../packaging/licenses/napi-rs-canvas-1.0.13/NOTICE.txt)；版本改变必须重新核对。

## 随程序附带的数据

- `internal/modelcatalog/bundled.json`：由 `go run ./internal/modelcatalog/gen` 从 [models.dev](https://models.dev)（MIT）的公开数据整理出的模型目录快照，只保留模型名、上下文长度、最大输出、输入类型和思考方式。
  设置里“更新模型目录”会下载同一来源的最新数据，存到用户数据目录，不改动安装目录。

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
| `@dicebear/core` | 10.7.0 | MIT |
| `@tanstack/react-query` | 5.104.2 | MIT |
| `mdast-util-from-markdown` | 2.1.0 | MIT |
| `highlight.js` | 11.12.0 | BSD-3-Clause |
| `@office-kit/pptx` | 0.28.0 | MIT |
| `@office-kit/pptx-preview` | 0.21.0 | MIT；按实际浏览器构建范围收集传递依赖声明 |
| `docx-preview` | 0.4.1 | Apache-2.0 |
| `xlsx`（SheetJS 官方发布包） | 0.20.3 | Apache-2.0 |
| `pdfjs-dist` | 6.4.299 | Apache-2.0 |
| `i18next` | 24.2.3 | MIT |
| `lucide-react` | 0.475.0 | ISC |
| `react` / `react-dom` | 18.3.1 | MIT |
| `react-i18next` | 15.7.4 | MIT |
| `react-markdown` | 9.1.0 | MIT |
| `react-router-dom` | 7.18.4 | MIT |
| `remark-gfm` | 4.0.1 | MIT |

表格使用锁文件中的确切版本，不使用 `package.json` 的版本范围代替实际版本。
`highlight.js` 在独立 worker 中处理有界源码页；`pdfjs-dist` 用于 PDF 预览，解析 worker、字体、CMap、ICC 与 WASM 资源随构建分发，不从 CDN 获取。
`xlsx` 从 SheetJS 官方 CDN 锁定发布包，预览解析在可终止的 worker 中执行；只显示保存值，不执行宏、外部链接或公式重算。
`react-markdown`、`remark-gfm`、`pdfjs-dist` 等会引入传递依赖，完整清单由声明脚本收集。
Vite、TypeScript、Tailwind、Vitest、Playwright 和接口生成工具等开发依赖单独由各自锁文件维护。

## 头像素材与打包资源

头像渲染库和头像素材是两类来源：

- `@dicebear/core` 是前端代码依赖，见上表。
- 团子系列使用来自 `@dicebear/styles` 10.6.0 的 Gaze 素材，其声明为 CC0 1.0。
- CoBot 补充的形状、色板和角色默认值采用仓库许可证 Apache-2.0。
- 来源及许可证见 [Dango LICENSE](../../assets/avatars/dango/LICENSE.md)。
  `dist/dango.json` 自带素材声明；头像系列由核心下载和缓存，不应被误记为内嵌的完整素材包。
- Windows MSI 中的 WiX UI 模板和资源另有随包声明，来源保存在
  [packaging/licenses/wix](../../packaging/licenses/wix)。
- 应用图标、界面标识与 favicon 随程序打包，来源和导出方式见
  [品牌资源说明](../../assets/branding/README.md)，不通过头像库下载。
  Python/Pillow 只用于重新导出品牌图像，日常构建复用已提交的图标，不要求安装 Python。

升级头像系列、替换素材或改变下载/内嵌方式时，要同时检查素材来源和声明，不能只更新 npm 依赖表。

## 外部运行条件

用户需安装 ChatGPT 桌面端；CoBot 只复用其中的 Codex 程序，不将引擎放入发行包，也不使用独立 CLI。发现方式与私有环境边界见[引擎适配规则](../rules/engine.md)。
Windows 桌面依赖已有 WebView2 Runtime；macOS 原生构建依赖平台工具链。
LaTeX 当前可使用明确提供的 macOS Tectonic 离线资源：启动环境变量 `COBOT_LATEX_BINARY` 和 `COBOT_LATEX_CACHE` 均需为绝对路径，前者指向可执行文件，后者指向完整的既有 Tectonic 缓存。临时输出只写当前数据根的缓存目录，退出后删除；不继承编译器密钥，不自动安装或下载。按[现有决定](../product/decisions.md)，本轮保留这些条件，暂缓资源包正式分发及其他平台隔离编译，不作为文件系统交付的阻塞项；不宣称发行包内置编译资源或已支持其他平台。

这些外部条件与内嵌依赖分开说明，不将“需要用户安装”写成“由 CoBot 分发”。

## 如何更新

1. 按锁文件安装依赖，核对目标平台与构建标签。
2. 对照 `go.mod` 和前端锁文件更新直接依赖表，并读取匹配版本的 LICENSE。
3. 使用目标平台打包脚本生成第三方正文，检查遗漏；不能用旧包的声明替换新包。
4. 改动涉及头像等素材时，同步其来源和许可证文件。
5. 在 PR 中说明范围和实际检查结果，不将历史 `npm audit` 结果写成当前无风险保证。

在依赖齐备的目标平台构建前端后，可单独生成声明用于复核：

```sh
node scripts/third-party-notices.mjs .tmp/THIRD-PARTY-NOTICES.txt
```

正式打包脚本会在输出路径后传入全部待分发的二进制路径，额外校验嵌入记录。`node_modules` 不随前端或桌面包分发。

macOS 本地试打包已核对前端构建记录、嵌入二进制和随包声明。Windows 打包脚本使用同一校验，仍需在 Windows 实际构建验收。
