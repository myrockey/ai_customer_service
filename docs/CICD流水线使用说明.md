# 智能客服系统 CI/CD 流水线使用说明

## 概述

本系统采用 GitLab CI/CD 实现自动化构建、测试和部署，支持多环境分离（测试环境/生产环境）、人工确认上线、自动回滚等企业级特性。

## 架构设计：CI/CD 与部署脚本的关系

### 设计原则：单一数据源（Single Source of Truth）

部署逻辑**只维护一份**（`deploy/deploy.sh` 和 `deploy/rollback.sh`），`.gitlab-ci.yml` 只负责**编排和触发**，通过 SSH 调用服务器上的脚本执行实际部署。

```
┌─────────────────────────────────────────────────────────────┐
│                      GitLab CI/CD                             │
│  .gitlab-ci.yml（只负责编排，不包含部署逻辑）                 │
│                                                              │
│  lint → test → build → docker_build                         │
│                          │                                   │
│                          ▼                                   │
│              ┌─────────────────────┐                        │
│              │  1. scp 同步脚本     │  确保服务器脚本最新    │
│              │  2. ssh 执行脚本     │  调用统一部署逻辑      │
│              └─────────────────────┘                        │
└─────────────────────────────────────────────────────────────┘
                          │
                          │ SSH 调用
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                      目标服务器                               │
│  /opt/cs-customer-service/                                   │
│  ├── deploy/                                                 │
│  │   ├── deploy.sh      ← 部署逻辑唯一数据源                │
│  │   └── rollback.sh    ← 回滚逻辑唯一数据源                │
│  ├── docker-compose.yaml                                    │
│  └── .env                                                   │
└─────────────────────────────────────────────────────────────┘
```

### 为什么这样设计？

| 问题 | 解决方案 |
|---|---|
| CI 内联脚本与手动部署脚本逻辑重复 | 统一调用 deploy.sh，只维护一份 |
| CI 部署和手动部署行为不一致 | 使用相同脚本，行为完全一致 |
| 修改部署逻辑需要同时改两处 | 只改 deploy.sh，CI 自动同步 |
| 部署脚本调试困难 | 在服务器上手动执行调试，调试通过后 CI 也能用 |

### .gitlab-ci.yml 部署任务的执行流程

```
1. scp 同步脚本
   ├─ deploy/deploy.sh → /opt/cs-customer-service/deploy/deploy.sh
   └─ deploy/rollback.sh → /opt/cs-customer-service/deploy/rollback.sh

2. SSH 远程执行
   ├─ cd /opt/cs-customer-service
   ├─ chmod +x deploy/*.sh
   ├─ docker login（登录镜像仓库，供 deploy.sh 拉取镜像）
   └─ ./deploy/deploy.sh <GO_IMAGE> <AGENT_IMAGE>
```

### deploy.sh 内部执行的 6 步流程

```
1. 预部署检查    →  服务状态 + 磁盘空间（≥5GB）
2. 保存当前版本   →  记录镜像 ID 到 .last_good_images（用于回滚）
3. 数据库备份     →  MySQL + PostgreSQL，压缩，保留7天
4. 拉取最新镜像   →  从镜像仓库拉取，标记为 latest
5. 滚动更新服务   →  先 agent 后 go，减少中断
6. 健康检查验证   →  30次重试，Go + Agent 双检查，清理悬空镜像
```

### 两种部署方式对比

| 维度 | CI/CD 自动部署 | 手动部署 |
|---|---|---|
| **触发方式** | GitLab Runner 自动/人工点击 | SSH 登录服务器手动执行 |
| **执行命令** | `./deploy/deploy.sh <镜像>` | `./deploy/deploy.sh <镜像>` |
| **部署逻辑** | 完全相同（同一个脚本） | 完全相同（同一个脚本） |
| **脚本同步** | CI 自动 scp 同步最新脚本 | 需手动 git pull 或 scp 同步 |
| **适用场景** | 日常版本发布、MR 合并自动部署 | 紧急修复、调试、自定义镜像部署 |

## 流水线架构

```
代码提交
    │
    ▼
┌─────────┐
│  lint   │  代码静态检查（Go + Python + Dockerfile）
└─────────┘
    │
    ▼
┌─────────┐
│  test   │  单元测试 + 覆盖率报告
└─────────┘
    │
    ▼
┌─────────┐
│  build  │  Go 交叉编译 + Python 依赖检查
└─────────┘
    │
    ▼
┌──────────────┐
│ docker_build │  构建镜像 + 推送镜像仓库
└──────────────┘
    │
    ├──────────────────┐
    ▼                  ▼
┌──────────────┐  ┌──────────────────┐
│ deploy_staging │  │ deploy_production │
│  (自动，develop)│  │ (人工确认，main) │
└──────────────┘  └──────────────────┘
```

## 流水线阶段说明

### 1. lint（代码静态检查）

| 任务 | 工具 | 触发条件 | 阻断 |
|---|---|---|---|
| golangci_lint | golangci-lint v2.6.2 | MR/main/develop | 是 |
| python_lint | ruff + flake8 | MR/main/develop | 是 |
| dockerfile_lint | hadolint | MR/main/develop | 否 |

**Go 检查项**：errcheck、gosimple、govet、ineffassign、staticcheck、unused、gofmt、goimports、misspell、gosec 等 18 项。

**Python 检查项**：pycodestyle、pyflakes、isort、pep8-naming、pyupgrade、flake8-bugbear、flake8-simplify 等。

### 2. test（单元测试）

| 任务 | 覆盖率 | 报告 |
|---|---|---|
| go_unit_test | 自动统计 | Cobertura XML |
| python_unit_test | 自动统计 | Cobertura XML |

测试结果会在 GitLab MR 页面显示覆盖率变化。

### 3. build（构建）

| 任务 | 产物 |
|---|---|
| build_go_binary | `cs_go_service` Linux amd64 二进制 |
| python_dependency_check | 依赖完整性验证 |

Go 二进制嵌入版本信息：
- `Version`：Git commit short SHA
- `BuildTime`：构建时间

### 4. docker_build（镜像构建）

| 镜像 | Dockerfile | 标签 |
|---|---|---|
| cs-go | `go_service/Dockerfile` | `$CI_COMMIT_SHORT_SHA` + `latest` |
| cs-agent | `python_agent/Dockerfile` | `$CI_COMMIT_SHORT_SHA` + `latest` |
| cs-nginx | 动态生成 | `$CI_COMMIT_SHORT_SHA` + `latest`（手动） |

镜像推送到 GitLab 容器镜像仓库：`$CI_REGISTRY_IMAGE/`

### 5. deploy_staging（测试环境部署）

- **触发**：自动（develop 分支）
- **部署方式**：SSH 远程调用服务器上的 `deploy/deploy.sh` 脚本
- **执行流程**：
  1. scp 同步最新 `deploy.sh` / `rollback.sh` 到服务器
  2. SSH 远程登录，登录镜像仓库
  3. 执行 `./deploy/deploy.sh <GO_IMAGE> <AGENT_IMAGE>`
- **deploy.sh 内部步骤**：预检查 → 保存版本 → 数据库备份 → 拉取镜像 → 滚动更新 → 健康检查

### 6. deploy_production（生产环境部署）

- **触发**：人工确认（main 分支/tags）
- **部署方式**：SSH 远程调用服务器上的 `deploy/deploy.sh` 脚本
- **执行流程**：
  1. scp 同步最新 `deploy.sh` / `rollback.sh` 到服务器
  2. SSH 远程登录，登录镜像仓库
  3. 执行 `./deploy/deploy.sh <GO_IMAGE> <AGENT_IMAGE>`
- **deploy.sh 内部步骤**：
  1. 预部署检查（磁盘空间、服务状态）
  2. 保存当前版本（用于回滚）
  3. 数据库备份（MySQL + PostgreSQL，压缩，保留7天）
  4. 拉取最新镜像
  5. 滚动更新服务（先 agent 后 go，减少中断）
  6. 健康检查（30次重试，Go + Agent 双健康检查）
  7. 清理悬空镜像

### 7. rollback_production（生产环境回滚）

- **触发**：人工确认
- **回滚方式**：SSH 远程调用服务器上的 `deploy/rollback.sh` 脚本
- **回滚目标**：上一个版本（从 `.last_good_images` 读取）
- **支持**：手动指定回滚镜像（服务器上执行 `./deploy/rollback.sh <GO_IMAGE> <AGENT_IMAGE>`）
- **CI 环境**：通过 `echo "y" | ./deploy/rollback.sh` 自动确认回滚

## GitLab CI/CD 变量配置

在 GitLab 项目 `Settings → CI/CD → Variables` 中配置以下变量：

### 镜像仓库（必填）

| 变量名 | 说明 | 示例 |
|---|---|---|
| `CI_REGISTRY` | 镜像仓库地址 | `registry.example.com` |
| `CI_REGISTRY_USER` | 镜像仓库用户名 | `gitlab+deploy-token` |
| `CI_REGISTRY_PASSWORD` | 镜像仓库密码 | `xxxxxxxx` |
| `CI_REGISTRY_IMAGE` | 镜像仓库路径 | `registry.example.com/group/project` |

### 测试环境（必填）

| 变量名 | 说明 | 示例 |
|---|---|---|
| `STAGING_SSH_HOST` | 测试环境服务器地址 | `staging.example.com` |
| `STAGING_SSH_USER` | SSH 用户名 | `deploy` |
| `STAGING_SSH_PRIVATE_KEY` | SSH 私钥（文件类型变量） | `-----BEGIN RSA PRIVATE KEY-----...` |

### 生产环境（必填）

| 变量名 | 说明 | 示例 |
|---|---|---|
| `PROD_SSH_HOST` | 生产环境服务器地址 | `prod.example.com` |
| `PROD_SSH_USER` | SSH 用户名 | `deploy` |
| `PROD_SSH_PRIVATE_KEY` | SSH 私钥（文件类型变量） | `-----BEGIN RSA PRIVATE KEY-----...` |

### 变量类型说明

- `SSH_PRIVATE_KEY` 系列变量建议使用 **File** 类型，流水线中通过 `$STAGING_SSH_PRIVATE_KEY` 直接引用文件路径
- 敏感变量（密码、私钥）勾选 `Mask variable` 和 `Protect variable`

## 服务器环境准备

### 1. 安装 Docker 和 Docker Compose

```bash
# 安装 Docker
curl -fsSL https://get.docker.com | bash

# 安装 Docker Compose 插件
apt-get install -y docker-compose-plugin

# 验证
docker --version
docker compose version
```

### 2. 创建部署用户和目录

```bash
# 创建部署用户
useradd -m -s /bin/bash deploy
usermod -aG docker deploy

# 创建部署目录
mkdir -p /opt/cs-customer-service
chown -R deploy:deploy /opt/cs-customer-service

# 创建备份目录
mkdir -p /backup
chown -R deploy:deploy /backup
```

### 3. 配置 SSH 免密登录

```bash
# 在 GitLab Runner 服务器生成密钥
ssh-keygen -t rsa -b 4096 -C "gitlab-ci-deploy"

# 将公钥添加到目标服务器
ssh-copy-id -i ~/.ssh/id_rsa.pub deploy@prod.example.com

# 验证免密登录
ssh deploy@prod.example.com "echo OK"
```

### 4. 上传项目文件到服务器

首次部署需要将以下文件上传到 `/opt/cs-customer-service`：

```
/opt/cs-customer-service/
├── docker-compose.yaml
├── .env                      # 环境变量配置
├── deploy/
│   ├── .env                  # Agent 环境变量
│   ├── nginx.conf            # Nginx 配置
│   ├── init/                 # 数据库初始化脚本（mysql/、postgres/）
│   ├── observability/        # 可观测性配置
│   ├── deploy.sh             # 部署脚本
│   └── rollback.sh           # 回滚脚本
└── web/                      # 前端静态文件（如果使用 Nginx 镜像）
```

### 5. 配置环境变量

复制 `.env.example` 为 `.env`，修改为实际配置：

```bash
cd /opt/cs-customer-service
cp .env.example .env
vim .env  # 修改数据库密码、API Key 等
```

### 6. 首次启动

```bash
cd /opt/cs-customer-service
docker compose up -d

# 查看服务状态
docker compose ps

# 查看日志
docker compose logs -f
```

## 分支策略

| 分支 | 用途 | 流水线 | 部署环境 |
|---|---|---|---|
| `main` | 生产稳定版本 | 全流程 | 生产（人工确认） |
| `develop` | 开发集成分支 | 全流程 | 测试（自动） |
| `feature/*` | 功能开发分支 | lint + test | 不部署 |
| `hotfix/*` | 紧急修复分支 | lint + test | 不部署 |
| `tags/v*` | 版本发布 | 全流程 | 生产（人工确认） |

## 常用操作

### 查看流水线状态

GitLab 项目 → `CI/CD → Pipelines`，可以查看每个阶段的状态、日志和产物。

### 手动触发部署

1. 进入 GitLab 项目 → `CI/CD → Pipelines`
2. 找到对应分支的流水线
3. 点击 `deploy_production` 任务的 `Play` 按钮
4. 确认部署

### 回滚操作

**方式一：GitLab CI 回滚**
1. 进入 GitLab 项目 → `CI/CD → Pipelines`
2. 找到对应版本的流水线
3. 点击 `rollback_production` 任务的 `Play` 按钮

**方式二：服务器手动回滚**
```bash
ssh deploy@prod.example.com
cd /opt/cs-customer-service
./deploy/rollback.sh
```

### 查看部署历史

```bash
ssh deploy@prod.example.com
cd /opt/cs-customer-service
cat .last_good_images
ls -lh /backup/
```

### 数据库恢复

```bash
# MySQL 恢复
docker exec -i cs_mysql mysql -uroot -p123456 cs_biz_db < /backup/mysql_20240101_120000.sql

# PostgreSQL 恢复
docker exec -i cs_postgres pg_restore -U cs_user -d cs_agent_db -c < /backup/postgres_20240101_120000.dump
```

## 监控与告警

### 部署状态通知

建议配置 GitLab CI 通知：
- 流水线失败 → 企业微信/钉钉/飞书机器人通知
- 生产部署成功 → 通知运维群
- 生产部署失败 → 电话/短信告警

### 服务健康监控

生产环境部署后，建议配置：
- **Prometheus**：监控 Go 服务 `/metrics` 端点
- **Grafana**：配置服务可用性仪表盘
- **AlertManager**：配置服务宕机、高延迟、错误率告警

详见 `docs/运维监控指南.md`

## 最佳实践

### 1. 数据库迁移

- 所有数据库变更必须通过迁移脚本，禁止手动修改生产数据库
- 初始化/迁移脚本统一放在 `deploy/init/`：
  - MySQL：`deploy/init/mysql/init.sql`
  - PostgreSQL：`deploy/init/postgres/init.sql`
- 生产部署包内副本：`deploy/production/deploy/init/`（随生产 compose 一起分发，部署前由 `docker-entrypoint-initdb.d` 自动执行）
- 备份/恢复脚本：`deploy/backup/`
- 部署前先在测试环境验证迁移脚本

### 2. 配置管理

- 敏感配置（数据库密码、API Key）通过环境变量注入，不写入代码仓库
- 使用 `.env.example` 作为配置模板，实际配置在服务器上维护
- 配置变更需要记录变更日志

### 3. 发布流程

```
开发 → feature 分支 → MR → lint/test 通过 → 合并 develop → 测试环境自动部署 → 验证 → 合并 main → 生产人工确认部署 → 验证
```

### 4. 紧急修复

```
hotfix 分支 → 直接基于 main → 修复 → MR → 快速验证 → 合并 main → 紧急部署
```

### 5. 版本管理

- 使用语义化版本：`v1.0.0`、`v1.1.0`、`v1.1.1`
- 每个版本打 Git tag，触发生产部署
- 版本发布说明记录在 `CHANGELOG.md`

## 故障排查

### 流水线失败

1. **lint 失败**：查看 lint 任务日志，修复代码风格问题
2. **test 失败**：查看测试报告，修复单元测试
3. **docker_build 失败**：检查 Dockerfile 语法、依赖下载
4. **deploy 失败**：查看部署日志，检查 SSH 连接、服务器磁盘空间、Docker 服务状态

### 手动部署（紧急修复，不走 CI/CD）

```bash
# 1. SSH 登录服务器
ssh deploy@prod.example.com

# 2. 进入部署目录
cd /opt/cs-customer-service

# 3. 确保部署脚本是最新版本（可选）
git pull  # 如果服务器上有 git 仓库

# 4. 登录镜像仓库
docker login registry.example.com

# 5. 执行部署脚本（指定镜像）
./deploy/deploy.sh registry.example.com/cs-go:v1.2.0 registry.example.com/cs-agent:v1.2.0

# 6. 验证部署
docker compose ps
curl http://localhost:8080/api/auth/tenants
```

### 手动回滚

```bash
# 方式一：自动回滚到上一个版本
ssh deploy@prod.example.com
cd /opt/cs-customer-service
./deploy/rollback.sh

# 方式二：回滚到指定版本
./deploy/rollback.sh registry.example.com/cs-go:v1.1.0 registry.example.com/cs-agent:v1.1.0

# 方式三：查看部署历史
cat .last_good_images
ls -lh /backup/
```

### 部署后服务异常

1. 查看服务状态：`docker compose ps`
2. 查看服务日志：`docker compose logs go agent`
3. 健康检查：`curl http://localhost:8080/api/auth/tenants`
4. 回滚：`./deploy/rollback.sh`

### 镜像拉取失败

1. 检查镜像仓库登录状态：`docker login $CI_REGISTRY`
2. 检查镜像是否存在：`docker pull $IMAGE`
3. 检查网络连接：`ping $CI_REGISTRY`

### 部署脚本更新

部署脚本纳入 Git 版本管理，修改后：
1. 提交代码：`git add deploy/deploy.sh && git commit -m "更新部署脚本"`
2. CI 流水线会自动 scp 同步最新脚本到服务器
3. 手动部署时需先 `git pull` 或手动 scp 同步

## 相关文件

| 文件 | 说明 | 职责 |
|---|---|---|
| `.gitlab-ci.yml` | GitLab CI/CD 流水线定义 | 编排：lint/test/build/docker_build/deploy |
| `go_service/.golangci.yml` | Go 代码检查配置 | lint 阶段使用 |
| `python_agent/pyproject.toml` | Python 代码检查配置 | lint 阶段使用 |
| `deploy/deploy.sh` | 统一部署脚本 | **部署逻辑唯一数据源**，CI 和手动部署都调用 |
| `deploy/rollback.sh` | 统一回滚脚本 | **回滚逻辑唯一数据源**，CI 和手动回滚都调用 |
| `docker-compose.yaml` | Docker Compose 服务编排 | 服务定义和配置 |
| `.env.example` | 环境变量配置模板 | 服务器环境配置 |
