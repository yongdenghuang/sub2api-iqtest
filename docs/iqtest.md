# 逐账号定时质量测试

本功能在 Sub2API 账号测试能力上增加独立的质量测试计划、结果和标记。
请求固定到被测账号，不因失败切换到其他账号。质量标记不会自动停用、
移组或恢复账号。测试请求仍会消耗上游额度。

## 已实现的测试

- **糖果题**：默认检查可见回复中独立的 `21`，可选择最终答案模式。
- **鹈鹕 SVG**：检查 SVG 结构、安全属性和图形数量，并提供静态启发式分数、图片预览。
  静态检查不能确认画面确实是鹈鹕骑自行车；视觉模型审核尚未实现。
- 可配置提示词、模型和超时。`max_tokens` 覆盖目前仅用于 Claude 测试路径，
  其他路径使用各自的上游实现。

## 首次使用

1. 使用管理员账号进入侧边栏的「智商测试」。
2. 新建一个**不启用定时**的计划，先选一个已知正常的账号和糖果题。
3. 点击「立即运行」，在「运行记录」中查看明细、实际输出和错误分类。
4. 验证结果后按需要加入 SVG 测试，设置范围、周期和异常判定，再启用计划。

运行明细支持分页，可以查看第 200 条之后的记录。历史结果会按账号和测试项的
`keep_results` 配置清理，因此旧运行的统计总数可能大于仍保留的结果数。
账号历史入口显示最近 100 条，运行明细分页不受这个显示上限影响。

## 范围、时间与判定

- 范围为全部、指定分组或指定账号。全部和分组查询只展开 active 账号；指定账号
  会先做适用性检查，禁用、冷却中或不支持自定义题目的账号记为 skipped。
- 分组和账号 ID 必须为正数；无效 ID 不能退化成全账号查询。
- 间隔为 10 分钟到 30 天，默认 6 小时。
- Cron 必须为五段表达式，使用服务器配置的时区，例如 `0 3 * * *`。
  不接受 `@every`、`@daily`、秒字段或表达式内时区覆盖。无法产生下一次运行时间
  的表达式（例如二月三十一日）不能启用；已有无效计划到期时停止调度，需编辑修正。
- 调度器每分钟检查到期计划，不承诺秒级启动时间。
- 只有 pass 和 quality_fail 参与连续失败或通过率计算；infra_error、skipped 不参与。
- 质量标记可以在连续通过后自动解除，也可以手动清除。这只影响质量标记，
  不等于恢复 Sub2API 账号的 error、限流或调度状态。

复用的官方账号测试路径可能在 403、429 等情况下更新账号错误或限流状态。
这些仍记录为请求故障，不算质量失败；本功能不调用测试成功后的账号自动恢复。
暂不包含「403 先复测一次」选项。

## 接入点与升级核对

新增逻辑集中于 `backend/internal/iqtest/` 和 `frontend/src/views/admin/iqtest/`，
使用自己的 SQL 表和迁移，不新增 Ent schema，也不修改 accounts 表结构。
升级官方代码时核对以下少量挂钩，并重新编译测试：

| 位置 | 作用 |
| --- | --- |
| `backend/internal/server/router.go` | 调用独立的 `iqtest_mount.go`，接入管理员认证和审计 |
| `backend/internal/service/account_test_service.go` | `ApplyAccountTestOverride` 注入 Claude 测试提示词和 token 上限 |
| `backend/internal/handler/admin/account_handler_iqtest.go` | 提供现有账号服务与测试服务依赖 |
| `frontend/src/router/index.ts` | 注册管理页面 |
| `frontend/src/components/layout/AppSidebar.vue` | 增加入口 |
| `frontend/src/views/admin/AccountsView.vue` | 合并显示质量标记 |
| `frontend/src/i18n/locales/{en,zh}/index.ts` | 合并独立文案 |

本轮没有改变调度器现有的多实例锁机制：Redis 不可用时仍降级为进程内互斥，
锁固定三小时且不续租。因此多实例部署或超长计划仍需进一步完善锁和运行恢复，
不能据此假定全局恰好执行一次。

## 验证

```sh
cd backend
go test ./internal/iqtest
go vet ./internal/iqtest
cd ../frontend
pnpm install --frozen-lockfile
pnpm exec vitest run src/views/admin/iqtest/__tests__/IQTestView.spec.ts
pnpm run build
```

以上是离线回归和编译验证。真实 PostgreSQL/Redis 部署、上游账号请求及计费表现，
需要在部署环境另做单账号试跑；不要把单元测试通过当作真实上游调用验证。
