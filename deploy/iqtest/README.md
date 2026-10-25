# IQTest 定制版的官方版本更新

生产分支：`feature/iqtest`。上游：`Wei-Shaw/sub2api`。生产站点：`https://sapi.yongdh.top`。

## 自动更新流程

1. GitHub Actions 每天北京时间 06:31 检查上游最新正式 release（排除预发布和草稿；GitHub 排队可能延迟）。也可手动运行 **IQTest upstream sync**。
2. 如果该 release 已包含在生产分支中，不重复更新。否则从生产分支创建独立候选分支，用普通 Git merge 保留智商测试和账号页定制。冲突直接失败，生产分支与 VPS 不变。
3. 在候选提交上运行官方 CI（后端单元 / 集成测试、lint、前端检查、部署脚本等），再运行智商测试专项前端测试、更新失败处理测试和完整前端构建。
4. 构建 Linux amd64 定制镜像，发布到 `ghcr.io/yongdenghuang/sub2api-iqtest`。版本含 `-iqtest.<commit>`，使用不可变 digest 部署；不使用官方镜像或浮动 latest。
5. 只有全部检查通过，才将候选提交快进合入生产分支。若分支已被其他人更新，停止以避免覆盖。
6. VPS 拉取镜像成功后开启短暂维护页面，停止本项目 app，备份本项目 PostgreSQL 和 data，再切换镜像。数据库、Redis 和其他 VPS 应用不会被更新。
7. 容器健康检查和管理员只读检查确认智商测试接口、认证、统计、GPT-6 Astra 默认值正常后解除维护。不会为了验证部署实际调用模型。

直接推送生产分支也会触发同样的检查、镜像构建和自动部署。上游同步内部显式调用可复用工作流，不依赖 `GITHUB_TOKEN` 的 push 再次触发 Actions。

## 失败与恢复

- 合并 / CI / 构建 / 镜像拉取失败时不修改运行中的服务。
- 备份失败时启动原 app，不启动新镜像。
- 新版本健康检查失败时，在维护窗口内恢复数据库、data 和原 app。恢复失败则保留维护状态与备份，等待排查。
- 进入维护后的更新失败会写入 `/opt/sub2api-iqtest/UPDATE_BLOCKED`，阻止后续自动部署。确认故障已解决、备份可用和原站点健康后，由管理员删除此文件，再从 Actions 重跑失败工作流。
- 自动备份位于 `/opt/sub2api-iqtest/backups/auto-*`，保留最近三份；仅清理本定制镜像仓库的旧镜像，保留当前与上一版。不执行全局 Docker prune。
- 备份包含应用数据与配置，目录与文件仅 root 可读。此备份用于升级回退，不替代异地灾备。

正常切换时 Compose 额外读取 `compose.iqtest-image.json`；手工管理时应同时传入：

```sh
cd /opt/sub2api-iqtest
docker compose -f compose.yaml -f compose.iqtest-image.json ps
```

## 凭证和维护入口

Actions 的 `IQTEST_DEPLOY_KEY` 是单独生成的 SSH 密钥，服务器授权使用 `restrict` 和固定命令。它仅能把本仓库的 `@sha256:` 镜像交给 root 所有的部署程序，不能执行任意 shell、端口转发或交互登录。`IQTEST_KNOWN_HOSTS` 固定 VPS 主机公钥。

GHCR 拉取使用工作流当前运行的短期 `GITHUB_TOKEN`，通过 SSH 标准输入传递，临时 Docker 登录文件在拉取后删除。无需存储 GitHub 个人令牌或 VPS 登录密码。

服务器固定部署程序：`/usr/local/lib/sub2api-iqtest/update.py`；SSH 入口：`/usr/local/sbin/sub2api-iqtest-deploy`。更新此特权程序需要管理员单独安装，应用镜像不能自动替换它。

HTTPS Nginx 配置通过 `/www/sites/sapi.yongdh.top/iqtest-maintenance` 控制维护响应。切换期间不要手动移除此文件；数据库回退完成前网站必须保持维护状态。

定制构建禁止后台下载官方二进制做更新或回退，版本面板提供 Actions 更新记录入口，避免丢失定制功能。

## 人工处理边界

遇到上游破坏性接口调整、合并冲突、CI 失败或 GitHub 拒绝机器人修改 workflow 文件，需要先修复候选分支再发布。自动流程不会强制覆盖、忽略失败检查或改动账号凭证。
