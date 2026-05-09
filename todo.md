# 当前任务

状态：已完成
更新时间：2026-05-09 15:26

## 目标

修复统计页面请求明细列表的悬停提示行为：仅认证文件列在悬停时显示详细信息，其他列悬停时无浏览器原生提示。

## 现状与触发原因

现有行为：请求明细列表中时间、模型、来源、API Key、结果、费用、性能、Token、客户端 IP 等列带有 `title`，认证文件列没有 `title`。
触发原因：表格行渲染时 `title` 属性分配位置相反。
目标行为：认证文件单元格带有完整文本提示，其他请求明细单元格移除原生悬停提示。

## 假设与约束

- 悬停提示指浏览器基于 `title` 属性显示的原生提示。
- 仅调整请求明细列表单元格的悬停提示属性。
- 前端源码变更后同步生成 `static/management.html`。

## 必须修改

- [x] 修改 `management-ui/src/components/usage/RequestEventsDetailsCard.tsx` 中请求明细表格单元格的 `title` 属性。
- [x] 运行管理端构建同步 `static/management.html`。

## 仅验证

- [x] 管理端构建同步。
  验证方式：`cd management-ui && npm run build:management-html`
  通过标准：命令退出码为 0。
- [x] 服务端编译验证。
  验证方式：`go build -o /tmp/cli-proxy-api-test-output ./cmd/server && rm /tmp/cli-proxy-api-test-output`
  通过标准：命令退出码为 0。
- [x] 请求明细表格 `title` 属性分布检查。
  验证方式：`rg -n "<td|title=" management-ui/src/components/usage/RequestEventsDetailsCard.tsx -S`
  通过标准：请求明细数据单元格中仅认证文件列包含 `title={row.authFile}`。

## 明确不修改

- 不修改请求明细数据获取、筛选、分页和导出逻辑。
- 不修改认证文件映射规则。
- 不调整表格样式布局。

## 阻塞项

无。

## 进度记录

- 2026-05-09 15:23 创建当前任务；确认旧任务 Git 状态干净，旧 `todo.md` 检查项均已完成。
- 2026-05-09 15:23 定位缺陷来源为 `RequestEventsDetailsCard.tsx` 表格行单元格 `title` 属性分配错误。
- 2026-05-09 15:24 已移除请求明细其他单元格的 `title`，并给认证文件单元格添加 `title`。
- 2026-05-09 15:24 管理端构建同步通过：`cd management-ui && npm run build:management-html`。
- 2026-05-09 15:24 服务端编译验证通过：`go build -o /tmp/cli-proxy-api-test-output ./cmd/server && rm /tmp/cli-proxy-api-test-output`。
- 2026-05-09 15:26 源码属性分布检查通过：请求明细数据单元格中仅认证文件列保留 `title`。

## 下一步

提交前复查变更文件。
