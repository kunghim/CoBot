# 前端规则

触发：改 `frontend/**`。

- 写或改页面前，先看 `frontend/src/gallery` 的规则和组件示例，复用 `frontend/src/components/ui`，不另造相似组件。
- 新增或修改了组件，同步更新画廊源码；需要看效果时生成本机画廊：
  `npm run gallery --prefix frontend -- --out ../.tmp/gallery/index.html`（`--out` 相对 `frontend/`）。
- 界面文案同时改 `frontend/src/i18n/locales/zh-CN.json` 和 `en.json`，两边键一致（英文可多 `_one`/`_other`）。
  `node scripts/check-docs.mjs` 会检查两边的键是否一致；没人用的键它查不出来，删改文案时自己删掉。
- 错误提示使用消息键；图标按钮提供可访问名称。
- 界面状态以接口返回为准；服务端推送只是“该刷新了”的提示，可能丢，不从推送内容里拼状态。
- 不在正常流程里暴露技术诊断细节，除非能帮用户作出判断。
- 不加没有实现的按钮或入口。

做完：`npm run typecheck --prefix frontend`、`npm test --prefix frontend` 通过；界面改动在浏览器里实际看一遍，附截图说明。
