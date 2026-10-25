# IQTest 定制版的官方版本更新

生产分支：`feature/iqtest`。上游：`Wei-Shaw/sub2api`。
唯一自动部署目标：`192.220.12.234` / `https://api.yongdh.top`。

## 自动更新流程

1. GitHub Actions 每天北京时间 06:31 检查上游最新正式 release，排除预发布和草稿。也可手动运行 **IQTest upstream sync**。
2. 已包含的正式版本不重复合并。新版本从生产分支创建候选分支，用普通 Git merge 保留智商测试和账号页定制；冲突时停止，不修改生产服务。
3. 在候选提交上运行官方 CI、智商测试专项检查、部署失败回退测试和完整前端构建。
4. 构建 Linux amd64 定制镜像，发布到 `ghcr.io/yongdenghuang/sub2api-iqtest`。版本含 `-iqtest.<commit>`，以不可变 digest 部署。
5. 检查通过才将候选提交快进合入生产分支，再部署到 **api.yongdh.top**。生产分支被并发更新时停止，避免覆盖。
6. VPS 拉取镜像后，开启维护状态，停止本项目应用并备份实际数据库、配置和数据，再验证、切换新版本。

直接推送生产分支也会触发上述流程。上游同步显式调用可复用工作流，不依赖 `GITHUB_TOKEN` 的 push 再次触发 Actions。合并、CI、构建或镜像拉取失败时不修改运行中的服务。

## 凭证和更新边界

Actions 使用独立 SSH 密钥，服务器授权限定 `restrict` 和固定命令，仅接受本仓库的 `@sha256:` 镜像，不能执行任意 shell 或交互登录。主机公钥固定校验。

GHCR 拉取使用本次工作流的短期 `GITHUB_TOKEN`，通过 SSH 标准输入传递；临时 Docker 登录文件在拉取后删除。不保存 VPS 密码、网站管理员密码或 GitHub 个人令牌。管理员改密码和开启 MFA 不影响更新。

部署验证不会实际调用模型。定制构建禁止在后台直接下载官方二进制覆盖，以保留定制功能。特权部署程序需要管理员单独安装，应用镜像不能自动替换它。

## 旧 VPS 已退出自动部署

`38.55.111.50` / `sapi.yongdh.top` 已从发布工作流移除，后续更新不再连接该服务器，也不再是现用 VPS 更新的前置依赖。旧的 `IQTEST_DEPLOY_KEY` / `IQTEST_KNOWN_HOSTS` 不再使用。

仓库中的 `update.py` 保留历史 Docker 部署实现，并提供 `native_update.py` 依赖的命令与镜像校验函数；当前自动部署入口仅为 `native_update.py`。

## 现用 VPS 的安装与数据

`192.220.12.234` / `https://api.yongdh.top` 保持原来的 systemd 安装方式。工作流检查通过后，将已测试镜像交给该服务器；从镜像提取二进制和 resources 更新 `/opt/sub2api`。不会重建数据库容器或 Redis，也不会覆盖已有账号。

- 服务：`sub2api.service`，端口 `127.0.0.1:8081`（Nginx 转发目标）。显式使用 `/opt/sub2api/config.yaml`，保留原配置与签名密钥。
- 实际数据库：`sub2api-postgres` 容器内的 **`sub2api_v2`**，应用角色也是 `sub2api_v2`。Compose 默认的 `sub2api` 是空库，不能用它备份或恢复。
- 更新程序：`/usr/local/lib/sub2api-iqtest/native_update.py`，依赖同目录 `update.py`。固定 SSH 入口仍为 `/usr/local/sbin/sub2api-iqtest-deploy`。
- 独立的受限部署密钥：`IQTEST_VPS192_DEPLOY_KEY`；固定主机公钥：`IQTEST_VPS192_KNOWN_HOSTS`。不能执行任意 SSH 命令。
- 状态与备份：`/opt/sub2api-iqtest-migration/`，自动备份 `backups/native-*` 保留最近三份；首次迁移的 `backups/preflight-*` 另行保留。备份包含实际数据库、旧二进制、配置和 data/resources。
- Nginx 维护标记：`/etc/nginx/iqtest-api.yongdh.top-maintenance`。失败锁：`/opt/sub2api-iqtest-migration/UPDATE_BLOCKED`。

新版先在维护状态下启动，并用临时 systemd 配置关闭 OAuth 令牌自动刷新。健康、版本、IQ 接口认证和数据库检查通过后，逐项核对账号凭据、用户密码、余额、API Key 与签名密钥，再移除临时配置恢复正常启动。验证阶段失败可以恢复旧数据库和二进制；**恢复正常令牌刷新之后若启动失败，则保留当前数据与维护状态并停止更新**，避免回退到已被轮换失效的 refresh token。此时应排查启动问题，不能盲目恢复旧数据库。

首次迁移前已在隔离数据库、独立 Redis 和禁止外网访问的容器中验证兼容性。原站点来自不同分支，保留了它额外的数据库表；这些表不代表定制版保留原分支所有 UI 功能。已有 8 个 OpenAI OAuth 账号、1 个管理员、1 个 API Key 和 6 个分组均保留。

查看运行状态：

```sh
systemctl status sub2api --no-pager
cat /opt/sub2api-iqtest-migration/current-release.json
curl --fail https://api.yongdh.top/health
```
