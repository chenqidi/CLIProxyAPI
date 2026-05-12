# 当前任务

状态：已完成
更新时间：2026-05-12 11:21

## 目标

在使用统计页面的凭证统计表格中，在现有凭证、请求次数、成功率三列之外，增加总 Token 数和总花费两列。

## 现状与触发原因

现有行为：凭证统计表格只展示凭证名称、请求次数和成功率。
触发原因：当前行聚合只累计成功与失败请求数量，未累计明细中的 token 和按模型价格计算的费用。
目标行为：每个凭证行展示同一统计范围内的总 Token 数和总花费，展示方式与使用统计页面其他表格保持一致。

## 假设与约束

当前凭证统计基于 legacy 使用明细聚合。
总 Token 数使用明细 tokens.total_tokens，缺失时按输入、输出、reasoning、cached 分类求和。
总花费使用现有模型价格配置和费用计算工具。
前端源码变更后同步生成 static/management.html。

## 必须修改

- [x] 修改 CredentialStatsCard 的行聚合结构，累计总 Token 数和总花费。
- [x] 修改凭证统计表头与行渲染，新增总 Token 数和总花费两列。
- [x] 从 UsagePage 向 CredentialStatsCard 传入 modelPrices。
- [x] 运行管理端构建同步 static/management.html。

## 仅验证

- [x] TypeScript 与管理端构建。
  验证方式：cd management-ui && npm run build:management-html
  通过标准：命令退出码为 0。
- [x] 服务端编译验证。
  验证方式：go build -o /tmp/cli-proxy-api-test-output ./cmd/server && rm /tmp/cli-proxy-api-test-output
  通过标准：命令退出码为 0。
- [x] 凭证统计字段检查。
  验证方式：检查 CredentialStatsCard 中聚合字段、表头和单元格。
  通过标准：表格展示凭证、请求次数、成功率、总 Token 数、总花费五列。

## 明确不修改

服务端 usage API 保持现有数据结构，因为现有 legacy 明细已经包含 token 与模型信息。
价格配置界面保持现状，因为费用计算复用现有模型价格配置。
请求明细、模型统计、API 统计、导出导入逻辑保持现状，因为本次展示需求只涉及凭证统计表格。

## 阻塞项

无。

## 进度记录

- 2026-05-12 10:59 创建当前任务；旧 todo.md 状态为已完成，可以覆盖为当前任务。
- 2026-05-12 10:59 完成影响分析：必须同步修改 CredentialStatsCard 聚合与 UsagePage 传参；仅需验证管理端构建和服务端编译；相关统计模块保持现状。

- 2026-05-12 11:01 已修改 CredentialStatsCard 聚合与 UsagePage 传参；首次运行 npm run build:management-html 时，tsc 与 vite build 完成，sync:management-html 写入 static/management.html 返回 EROFS。

- 2026-05-12 11:05 管理端构建同步验证通过：npm run build:management-html，包含 tsc、vite build 与 static/management.html 同步。
- 2026-05-12 11:05 服务端编译首次因 Go 缓存目录只读失败，按环境审批机制重试后通过：go build -o /tmp/cli-proxy-api-test-output ./cmd/server。
- 2026-05-12 11:05 字段检查通过：CredentialStatsCard 表头包含凭证、请求次数、成功率、总 Token 数、总花费，行渲染包含 row.totalTokens 与 row.totalCost；static/management.html 已包含中英文总 Token 与总花费文案。
- 2026-05-12 11:05 git diff --check 通过，未发现空白字符问题。

- 2026-05-12 11:20 复查发现凭证总花费为 0 时显示 `$0.00` 与现有请求明细、模型统计不一致，已改为 `--`。
- 2026-05-12 11:20 重新运行管理端构建同步通过：npm run build:management-html。

- 2026-05-12 11:21 修正后服务端编译通过：go build -o /tmp/cli-proxy-api-test-output ./cmd/server。
- 2026-05-12 11:21 修正后字段与格式检查通过：git diff --check；凭证统计表格保留五列，费用为 0 时显示 `--`。

## 下一步

提交前复查变更文件。
