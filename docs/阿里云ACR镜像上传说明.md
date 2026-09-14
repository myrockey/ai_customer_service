# 阿里云 ACR 镜像打包上传脚本使用说明

## 概述

`deploy/push-to-acr.sh` 是一个独立的镜像打包上传脚本，用于将智能客服系统的 Docker 镜像构建并推送到阿里云容器镜像服务（ACR）。

支持的镜像：
- `cs-go`：Go 网关服务
- `cs-agent`：Python Agent 服务
- `cs-nginx`：Nginx 网关服务（可选）

## 版本管理策略

### 推荐方案：语义化版本 + latest 双标签

```
生产发布版本：v1.0.0（语义化，人工指定）
开发测试版本：a1b2c3d（Git Commit SHA，自动生成）
最新版本标签：latest（自动指向最新生产版本）
```

| 环境 | 版本号格式 | 生成方式 | 示例 | 是否打 latest |
|---|---|---|---|---|
| 开发/测试 | Git Commit SHA | CI 自动生成 | `a1b2c3d` | 否 |
| 生产发布 | 语义化版本 | 人工指定 | `v1.0.0` | 是 |
| 最新版本 | latest | 自动指向 | `latest` | - |

### 语义化版本规范

```
v1.0.0
│ │ └─ 补丁版本（bug修复，向后兼容）
│ └─── 次版本（新增功能，向后兼容）
└───── 主版本（不兼容的API变更）
```

**版本号递增规则**：
- 修复 bug：`v1.0.0` → `v1.0.1`
- 新增功能：`v1.0.0` → `v1.1.0`
- 重大变更：`v1.0.0` → `v2.0.0`

## 脚本联动：push-to-acr.sh + deploy.sh

### 联动架构

```
push-to-acr.sh 推送镜像
    │
    ├─ 1. 构建并推送镜像（tag=v1.0.0）
    ├─ 2. 同时打 latest 标签并推送（仅语义化版本）
    └─ 3. 写入版本文件 .image_versions
              │
              ▼
         deploy.sh 部署
              │
              ├─ 方式1（推荐）: ./deploy.sh              # 自动读取 .image_versions
              ├─ 方式2:         ./deploy.sh v1.0.0       # 指定版本号
              ├─ 方式3:         ./deploy.sh latest        # 使用 latest 标签
              └─ 方式4:         ./deploy.sh <GO_IMG> <AGENT_IMG>  # 指定完整镜像
```

### 版本文件格式

`.image_versions`（由 push-to-acr.sh 自动生成）：

```bash
# 智能客服系统镜像版本记录
# 自动生成，由 push-to-acr.sh 更新
# 部署时 deploy.sh 会读取此文件获取镜像版本
ACR_REGISTRY=crpi-xxx.cn-shenzhen.personal.cr.aliyuncs.com
ACR_NAMESPACE=customer-service
IMAGE_TAG=v1.0.0
GO_IMAGE=crpi-xxx.cn-shenzhen.personal.cr.aliyuncs.com/customer-service/cs-go:v1.0.0
AGENT_IMAGE=crpi-xxx.cn-shenzhen.personal.cr.aliyuncs.com/customer-service/cs-agent:v1.0.0
NGINX_IMAGE=crpi-xxx.cn-shenzhen.personal.cr.aliyuncs.com/customer-service/cs-nginx:v1.0.0
PUSH_TIME=2024-01-01 12:00:00
GIT_COMMIT=a1b2c3d
```

### 完整发布流程

```bash
# 1. 开发完成，提交代码
git add -A
git commit -m "feat: 新增xxx功能"
git push

# 2. 打版本标签
git tag -a v1.1.0 -m "Release v1.1.0: 新增xxx功能"
git push origin v1.1.0

# 3. 构建并推送镜像到阿里云 ACR
./deploy/push-to-acr.sh all v1.1.0
# 自动完成：
#   - 构建 cs-go、cs-agent、cs-nginx 镜像
#   - 推送 v1.1.0 标签
#   - 推送 latest 标签（语义化版本自动打）
#   - 写入 .image_versions 版本文件

# 4. 部署到生产环境（SSH 登录服务器）
ssh deploy@prod.example.com
cd /opt/cs-customer-service

# 方式1（推荐）：自动读取版本文件
./deploy/deploy.sh

# 方式2：指定版本号
./deploy/deploy.sh v1.1.0

# 方式3：使用 latest 标签
./deploy/deploy.sh latest

# 5. 验证部署
docker compose ps
curl http://localhost:8080/api/auth/tenants
```

### 回滚流程

```bash
# 部署出问题，一键回滚到上一个版本
ssh deploy@prod.example.com
cd /opt/cs-customer-service

# 方式1：自动回滚到上一个版本（从 .last_good_images 读取）
./deploy/rollback.sh

# 方式2：回滚到指定版本
./deploy/rollback.sh v1.0.0

# 方式3：查看部署历史
cat .last_good_images
ls -lh /backup/
```

## 快速开始

### 1. 修改 ACR 配置

编辑 `deploy/push-to-acr.sh`，修改以下配置：

```bash
# 阿里云 ACR 配置（根据实际情况修改）
ACR_REGISTRY="crpi-xxxxxxxxxxxx.cn-shenzhen.personal.cr.aliyuncs.com"  # 替换为你的 ACR 实例地址
ACR_NAMESPACE="customer-service"                                           # 命名空间
ACR_USERNAME=""                                                             # ACR 用户名（可选）
ACR_PASSWORD=""                                                             # ACR 密码（可选）
```

**获取 ACR 实例地址**：
1. 登录阿里云控制台 → 容器镜像服务 ACR
2. 进入「实例列表」→ 选择你的实例
3. 复制「访问域名」（如 `crpi-678abaftucmpao2w.cn-shenzhen.personal.cr.aliyuncs.com`）

### 2. 执行脚本

```bash
# 构建并推送全部镜像
./deploy/push-to-acr.sh all v1.0.0

# 构建并推送单个镜像
./deploy/push-to-acr.sh go v1.0.0
./deploy/push-to-acr.sh agent v1.0.0
./deploy/push-to-acr.sh nginx v1.0.0
```

## 使用方式

### 方式一：脚本内配置（推荐）

直接修改脚本中的 `ACR_REGISTRY`、`ACR_NAMESPACE` 等配置项，然后执行脚本。

### 方式二：环境变量覆盖

通过环境变量临时覆盖配置，不修改脚本：

```bash
# 临时指定 ACR 配置
export ACR_REGISTRY_OVERRIDE="crpi-xxxxxxxxxxxx.cn-shenzhen.personal.cr.aliyuncs.com"
export ACR_NAMESPACE_OVERRIDE="my-namespace"
export ACR_USERNAME_OVERRIDE="your-username"
export ACR_PASSWORD_OVERRIDE="your-password"

# 执行脚本
./deploy/push-to-acr.sh all v1.0.0
```

### 方式三：交互式登录

不配置用户名密码，执行脚本时会提示交互式输入：

```bash
$ ./deploy/push-to-acr.sh go v1.0.0
[STEP] [3/4] 登录阿里云镜像仓库...
Username: your-username
Password: ********
Login Succeeded
```

## 命令参数

```
用法: ./push-to-acr.sh <service|all> <tag>

参数：
  service    服务名称（go/agent/nginx/all）
  tag        镜像标签（如 v1.0.0、latest、20240101）

支持的服务：
  go         Go 网关服务
  agent      Python Agent 服务
  nginx      Nginx 网关服务
  all        全部服务

示例：
  ./push-to-acr.sh all v1.0.0          # 构建并推送全部镜像
  ./push-to-acr.sh go v1.1.0           # 只构建并推送 Go 镜像
  ./push-to-acr.sh agent latest         # 只构建并推送 Agent 镜像
```

## 执行流程

脚本对每个镜像执行以下 4 步：

```
1. 构建本地镜像
   docker build -f <Dockerfile> -t <tmp-image> .

2. 绑定仓库标签
   docker tag <tmp-image> <ACR_REGISTRY>/<NAMESPACE>/<IMAGE>:<TAG>

3. 登录阿里云镜像仓库
   docker login <ACR_REGISTRY>

4. 推送镜像
   docker push <ACR_REGISTRY>/<NAMESPACE>/<IMAGE>:<TAG>
```

## 镜像地址说明

执行完成后，镜像地址格式为：

```
<ACR_REGISTRY>/<ACR_NAMESPACE>/<IMAGE_NAME>:<TAG>
```

示例：

```
crpi-xxxxxxxxxxxx.cn-shenzhen.personal.cr.aliyuncs.com/customer-service/cs-go:v1.0.0
crpi-xxxxxxxxxxxx.cn-shenzhen.personal.cr.aliyuncs.com/customer-service/cs-agent:v1.0.0
crpi-xxxxxxxxxxxx.cn-shenzhen.personal.cr.aliyuncs.com/customer-service/cs-nginx:v1.0.0
```

## 在 docker-compose.yaml 中使用

修改 `docker-compose.yaml` 中的 `image` 字段，使用阿里云 ACR 镜像：

```yaml
services:
  go:
    image: crpi-xxxxxxxxxxxx.cn-shenzhen.personal.cr.aliyuncs.com/customer-service/cs-go:v1.0.0
    # build: ./go_service  # 注释掉本地构建

  agent:
    image: crpi-xxxxxxxxxxxx.cn-shenzhen.personal.cr.aliyuncs.com/customer-service/cs-agent:v1.0.0
    # build: ./python_agent  # 注释掉本地构建

  nginx:
    image: crpi-xxxxxxxxxxxx.cn-shenzhen.personal.cr.aliyuncs.com/customer-service/cs-nginx:v1.0.0
```

部署时：

```bash
# 登录 ACR
docker login crpi-xxxxxxxxxxxx.cn-shenzhen.personal.cr.aliyuncs.com

# 拉取镜像并启动
docker compose pull
docker compose up -d
```

## 与 CI/CD 流水线的关系

| 维度 | CI/CD 自动推送 | 手动脚本推送 |
|---|---|---|
| **触发方式** | GitLab Runner 自动触发 | 手动执行脚本 |
| **镜像仓库** | GitLab 容器镜像仓库 | 阿里云 ACR |
| **适用场景** | 日常开发、测试环境 | 生产发布、客户交付 |
| **脚本** | `.gitlab-ci.yml` | `deploy/push-to-acr.sh` |

**典型工作流**：

```
开发阶段：
  代码提交 → GitLab CI 自动构建 → 推送到 GitLab 镜像仓库 → 自动部署测试环境

发布阶段：
  版本发布 → 手动执行 push-to-acr.sh → 推送到阿里云 ACR → 客户服务器拉取部署
```

## 常见问题

### 1. 登录失败：unauthorized: authentication required

**原因**：用户名或密码错误，或者没有命名空间的权限。

**解决**：
- 确认 ACR 用户名和密码正确
- 确认该账号有对应命名空间的推送权限
- 个人版 ACR 需要先创建命名空间

### 2. 推送失败：denied: requested access to the resource is denied

**原因**：命名空间不存在，或者没有权限。

**解决**：
- 登录阿里云 ACR 控制台，确认命名空间已创建
- 确认命名空间名称与脚本中配置一致
- 个人版 ACR 命名空间全局唯一，可能已被占用

### 3. 构建失败：Dockerfile not found

**原因**：执行脚本的目录不对，或者 Dockerfile 路径错误。

**解决**：
- 在项目根目录执行脚本
- 确认 `go_service/Dockerfile` 和 `python_agent/Dockerfile` 存在

### 4. 镜像标签不合法

**原因**：标签包含特殊字符。

**解决**：
- 标签只允许字母、数字、点、下划线、短横线
- 示例：`v1.0.0`、`latest`、`20240101`、`dev-test`

### 5. 如何查看已推送的镜像？

登录阿里云 ACR 控制台 → 镜像仓库 → 选择命名空间 → 查看镜像列表和标签。

## 相关文件

| 文件 | 说明 | 职责 |
|---|---|---|
| `deploy/push-to-acr.sh` | 阿里云 ACR 镜像打包上传脚本 | 构建镜像 + 推送 ACR + 写入版本文件 |
| `deploy/deploy.sh` | 生产环境部署脚本 | 读取版本文件 + 拉取镜像 + 滚动更新 + 健康检查 |
| `deploy/rollback.sh` | 生产环境回滚脚本 | 读取 .last_good_images + 回滚到上一版本 |
| `.image_versions` | 镜像版本文件（自动生成） | 记录 ACR 配置、镜像地址、推送时间、Git Commit |
| `.last_good_images` | 上一版本记录（自动生成） | 记录当前运行的镜像 ID，用于回滚 |
| `.gitlab-ci.yml` | GitLab CI/CD 流水线定义 | 开发阶段自动构建推送到 GitLab 镜像仓库 |
| `docker-compose.yaml` | Docker Compose 服务编排 | 服务定义和配置 |

## 版本管理最佳实践

### 1. 版本号命名规范

```
生产版本：v1.0.0、v1.1.0、v2.0.0（语义化）
测试版本：test-20240101、test-a1b2c3d（带前缀）
开发版本：dev-a1b2c3d（Git Commit SHA）
紧急修复：v1.0.1-hotfix（带后缀）
```

### 2. 发布流程检查清单

```
发布前：
  [ ] 代码已合并到 main 分支
  [ ] 所有测试通过
  [ ] 已更新版本号（CHANGELOG.md）
  [ ] 已打 Git 标签（git tag v1.0.0）

发布中：
  [ ] 执行 push-to-acr.sh all v1.0.0
  [ ] 确认镜像推送成功
  [ ] 确认 .image_versions 文件已更新

发布后：
  [ ] SSH 登录服务器执行 deploy.sh
  [ ] 确认服务健康检查通过
  [ ] 验证核心功能正常
  [ ] 保存 .last_good_images（用于回滚）
```

### 3. 回滚触发条件

- 部署后健康检查失败
- 核心功能异常
- 性能严重下降
- 数据错误或丢失风险
- 用户反馈严重问题

### 4. 多环境版本管理

```
开发环境：
  - 镜像仓库：GitLab 容器镜像仓库
  - 版本号：Git Commit SHA
  - 部署：CI 自动部署

测试环境：
  - 镜像仓库：GitLab 容器镜像仓库
  - 版本号：test-xxx 或 Git Commit SHA
  - 部署：CI 自动部署 / 手动部署

生产环境：
  - 镜像仓库：阿里云 ACR
  - 版本号：语义化版本 v1.0.0
  - 部署：手动执行 deploy.sh
  - 回滚：手动执行 rollback.sh
```
