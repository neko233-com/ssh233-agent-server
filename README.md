# SSH233 Agent Server

Go 1.26 统一 SSH 跳板机 + 多租户 + Web SSH + Agent 集群 + HTTP API。

## 功能

- **多租户**：租户隔离主机/用户/Agent/审计；`root/root` 超级管理员跨租户管理
- **SSH 跳板机**：统一入口、操作审计、密钥自动上传
- **Web SSH**：`login.html` 登录 → `manager.html` 管理控制台（New API 风格垂直 Tab）
- **后台守护**：`start` / `stop` / `status`，Linux/macOS/Windows 均支持
- **开机自启（可选）**：安装默认不开启，需手动 `enable-autostart`
- **日志滚动**：slog 文件日志按大小自动轮转，避免磁盘占满
- **审计清理**：管理台可视化清理历史操作日志
- **数据库**：默认 SQLite 本地；可切换 MySQL 外部库
- **配置**：仅使用 `config.yaml`

## 一键安装

**Linux / macOS / Git Bash**

```bash
bash scripts/install.sh --from-source
# 或发布包
bash scripts/install.sh --version v0.2.0
```

**Windows PowerShell**

```powershell
.\scripts\install.ps1 -FromSource
# 或
.\scripts\install.ps1 -Version v0.2.0
```

安装后自动 **后台启动**（不开机自启）：

- Web: http://127.0.0.1:6030/login.html
- 管理员: `root` / `root`

## 服务管理

```bash
ssh233-server start   -config config.yaml   # 后台启动
ssh233-server stop    -config config.yaml
ssh233-server restart -config config.yaml
ssh233-server status  -config config.yaml
ssh233-server serve   -config config.yaml   # 前台调试

# 开机自启（可选，默认关闭）
ssh233-server enable-autostart  -config config.yaml
ssh233-server disable-autostart -config config.yaml
ssh233-server autostart-status  -config config.yaml
```

## 手动运行

```bash
cp config.example.yaml config.yaml
go run ./cmd/server -config config.yaml
```

## 配置示例

```yaml
server:
  http_addr: ":6030"
  ssh_addr: ":2222"

database:
  driver: sqlite          # sqlite | mysql
  sqlite:
    path: data/ssh233.db

auth:
  admin_user: root
  admin_password: root

logging:
  path: logs/ssh233.log
  max_size_mb: 10
  max_backups: 5
  max_age_days: 30
  level: info
```

## 多租户登录

| 用户类型 | 租户字段 | 用户名 | 密码 |
|---------|---------|--------|------|
| 超级管理员 | 留空 | root | root |
| 租户用户 | default / 租户 slug | 用户名 | 密码 |

SSH 跳板：`ssh user@tenant@bastion -p 2222` 或 Web 登录后选租户。

## 测试

```bash
go test ./... -count=1 -cover
go test ./... -count=1 -race          # Linux/macOS
go test ./internal/integration/... -count=1   # 含 daemon 集成测试
bash scripts/ci-smoke.sh              # 端到端冒烟
bash scripts/test.sh
powershell scripts/test.ps1
```

## API

```bash
TOKEN=$(curl -s -X POST http://localhost:6030/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"root","password":"root"}' | jq -r .token)

curl -H "Authorization: Bearer $TOKEN" http://localhost:6030/api/v1/tenants
curl -H "Authorization: Bearer $TOKEN" http://localhost:6030/api/v1/hosts
curl -H "Authorization: Bearer $TOKEN" http://localhost:6030/api/v1/audit/stats
curl -X DELETE -H "Authorization: Bearer $TOKEN" \
  "http://localhost:6030/api/v1/audit?older_than_days=30"
```

## 项目结构

```
cmd/server/       主服务 + CLI + 静态页面
cmd/agent/        示例 Agent
internal/
  config/         YAML 配置
  logging/        slog 滚动日志
  store/          SQLite + MySQL
  auth/           JWT + 多租户
  api/            REST API
  bastion/        SSH 跳板
scripts/          install / test / ci-smoke
```
