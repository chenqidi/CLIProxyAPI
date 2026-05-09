# 当前任务

状态：已完成
更新时间：2026-05-09 14:34

## 目标

将请求事件明细性能胶囊中的前缀文案去掉，例如“首字 8s”显示为“8s”。

## 现状与触发原因

现有行为：性能列每个胶囊显示“首字/用时/输出速率 + 数值”。
触发原因：截图反馈希望单元格内只保留数值，减少视觉冗余。
目标行为：表格胶囊内只显示 `row.firstTokenLabel`、`row.latencyLabel`、`row.outputRateLabel`；保留单元格 `title` 作为完整提示。

## 假设与约束

- 只调整请求明细表格的可见文本，不改统计数据、API、排序、筛选或导出。
- 表头“首字 / 用时 / 输出速率”和 hover title 保持不变，避免语义丢失。
- 前端改动后同步生成 `static/management.html`。

## 必须修改

- [x] 修改请求明细性能胶囊渲染文本。
- [x] 同步管理端静态构建产物 `static/management.html`。

## 仅验证

- [x] 管理端构建同步。
  验证方式：`cd management-ui && npm run build:management-html`
  通过标准：命令退出码为 0。
- [x] 服务端编译校验。
  验证方式：`go build -o test-output ./cmd/server && rm test-output`
  通过标准：命令退出码为 0。

## 明确不修改

- 请求事件 API、存储和统计计算。
- 请求明细表头、hover 完整说明、筛选、分页和导出行为。
- 其他页面的耗时/性能文案。

## 阻塞项

无。

## 进度记录

- 2026-05-09 14:33 创建当前任务；既有 `todo.md` 为已完成任务，可覆盖。
- 2026-05-09 14:33 已定位性能胶囊渲染位置：`management-ui/src/components/usage/RequestEventsDetailsCard.tsx`。
- 2026-05-09 14:33 已将性能胶囊可见文本改为只显示数值，完整语义保留在 `title`。
- 2026-05-09 14:34 管理端构建通过：`npm run build:management-html`，`static/management.html` 已同步更新。
- 2026-05-09 14:34 服务端编译校验通过：`go build -o test-output ./cmd/server && rm test-output`。
- 2026-05-09 14:34 源码核对通过：性能胶囊显示 `row.firstTokenLabel`、`row.latencyLabel`、`row.outputRateLabel`。

## 下一步

无。
