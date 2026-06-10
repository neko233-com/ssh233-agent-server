# SSH233 Agent Server

Go 1.26 统一 SSH 跳板机 + 多租户 + Web SSH + Agent 集群 + HTTP API。

## 功能

- **多租户**：租户隔离主机/用户/Agent/审计；`root/root` 超级管理员跨租户管理
- **SSH 跳板机**：统一入口、操作审计、密钥自动上传
- **Web SSH**：`login.html` 登录 → `manager.html` 管理控制台
- **数据库**：默认 SQLite 本地；可切换 MySQL 外部库
- **配置**：仅使用 `config.yaml`

## 一键安装

**Linux / macOS / Git Bash**

```bash
bash scripts/install.sh --from-source
# 或发布包
bash scripts/install.sh --version latest
```

**Windows PowerShell**

```powershell
.\scripts\install.ps1 -FromSource
```

安装后：

- Web: http://127.0.0.1:6030/login.html
- 管理员: `root` / `root`

## 手动运行

```bash
cp config.example.yaml config.yaml
go run ./cmd/server -config config.yaml
```

## 配置示例

```yaml
database:
  driver: sqlite          # sqlite | mysql
  sqlite:
    path: data/ssh233.db
  mysql:
    dsn: "user:pass@tcp(127.0.0.1:3306)/ssh233?parseTime=true&charset=utf8mb4"

auth:
  admin_user: root
  admin_password: root
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
# 或
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
```

## 项目结构

```
cmd/server/       主服务 + 静态页面 (login.html / manager.html)
cmd/agent/        示例 Agent
internal/
  config/         YAML 配置
  store/          SQLite + MySQL
  auth/           JWT + 多租户
  api/            REST API
  bastion/        SSH 跳板
scripts/          install.sh / install.ps1 / test.sh
```
