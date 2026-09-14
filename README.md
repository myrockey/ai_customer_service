<div align="center">

# 智能客服系统（多租户 SaaS 版）

**AI 自动应答 + 人工客服接管 + 知识库问答 + 多租户独立配置**

[![License: AGPL v3](https://img.shields.io/badge/License-AGPL%20v3-blue.svg)](./LICENSE)
[![Online Demo](https://img.shields.io/badge/Demo-在线体验-brightgreen)](https://cs.hwcom.top)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ed?logo=docker)
![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go)
![Python](https://img.shields.io/badge/Python-3.13-3776AB?logo=python)
![Vue3](https://img.shields.io/badge/Vue-3-4FC08D?logo=vuedotjs)

🚀 **在线体验（无需部署，直接看效果）：https://cs.hwcom.top**

</div>

> 多租户智能客服系统：**Go 网关 + Python LangGraph Agent + MySQL/PostgreSQL/Qdrant + Redis**，支持第三方网站 / App / 小程序多端接入，AI 自动应答 + 人工接管完整闭环，开箱即用。

---

## 📸 界面预览

| 平台管理员 · 租户管理 | 租户 · 工单管理（人工接管） | 终端用户 · SDK 在线客服 |
|---|---|---|
| ![平台后台](./deploy/cs_images/平台-租户管理.PNG) | ![租户工单](./deploy/cs_images/租户后台.PNG) | ![SDK演示](./deploy/cs_images/sdk演示聊天.PNG) |

> 📖 **图文操作手册**（10 张截图逐步讲解每个模块怎么操作）：[docs/README.md](./docs/README.md) · **5 分钟快速体验**：[docs/快速体验.md](./docs/快速体验.md)

## ✨ 核心能力

| 能力 | 说明 |
|------|------|
| 🤖 AI 智能对话 | LangGraph 编排、流式输出、RAG 知识库检索、工具调用 |
| 👨‍💼 人工客服 | 工单管理、会话查看、人工接管对话、实时推送通道 |
| 📚 知识库管理 | 文档上传、向量索引、语义检索、批量重索引、按租户隔离 |
| 📝 Prompt 管理 | 版本控制、回滚、租户级 + 平台级 Prompt 优先级 |
| 🏢 多租户 | 数据/知识库/模型配置隔离、用量配额、平台/租户双级管理 |
| 🔌 第三方集成 | SDK（cs-chat.js 浮窗）、iframe、API、WebSocket 四种接入方式 |
| 💰 用量计费 | Token 用量统计（按模型/来源），支持单价配置与费用核算 |
| 📊 可观测性 | Loki 日志 + Prometheus 指标 + Jaeger 链路 + Grafana 可视化 |
| 🚢 容器化部署 | Docker Compose 一键起全部服务，支持镜像打包 / CI-CD / 私有化部署 |

## 🚀 快速开始

```bash
# 1. 克隆项目
git clone https://github.com/myrockey/ai_customer_service.git && cd ai_customer_service

# 2. 配置环境变量
cp .env.example .env

# 3. 一键启动（Docker Compose）
docker compose up -d --build

# 4. 等待健康检查通过（约 1-2 分钟）
docker compose ps

# 5. 访问系统
# 管理后台   http://localhost:8080/admin/
# 用户端     http://localhost:8080/chat/pc.html
# SDK 示例   http://localhost:8080/sdk/
```

**默认账号**（开发环境）：

| 角色 | app_key | app_secret |
|------|---------|-----------|
| 平台管理员 | `admin_key` | `admin_secret` |
| 租户（demo） | `demo_key` | `demo_secret` |

> ⚠️ 登录后第一件事：到「平台模型配置」配置对话模型 + 向量模型，AI 对话才会生效（界面支持一键测试连通性）。

## 📜 授权与商业化

|  | 社区版（本仓库 · 开源） | 商业版（私有化交付） |
|--|--|--|
| 协议 | AGPL-3.0：学习 / 自用 / 非商业 / 继续开源 **免费** | 商业授权（[COMMERCIAL.md](./COMMERCIAL.md)） |
| 代码 | go_service / python_agent / web / admin-app / docs 全部开源 | 生产部署包（镜像 + 一键脚本 + 监控告警配置）闭源交付 |
| 适用 | 自部署、学习研究、二次开发 | 企业内部闭源部署、对外商业 SaaS、产品化销售 |
| 支持 | GitHub Issues | 部署支持 / 升级保障 / 定制开发（SSO、多实例等） |
| 获取 | 直接 clone | 邮箱 2771452556@qq.com |

> 支持开源作者：点 ⭐ Star、提 Issue、提交 PR 都是最好的支持 🙌

## 🏗️ 系统架构

```
用户浏览器 / 第三方网站
        │  SDK(cs-chat.js) / iframe / API / WebSocket
        ▼
┌──────────────────────────────────────────────────┐
│  Nginx (8080)  统一入口：静态前端 + API/WS 反代    │
└───────────────┬──────────────────────────────────┘
                ▼
┌──────────────────────────────────────────────────┐
│  Go 网关 (8080 内网)  Gin：鉴权/租户隔离/限流/     │
│  路由/工单/模型配置/用量报表/审计/消息归档          │
└───────┬──────────────────────┬──────────────────┘
        ▼                       ▼
┌──────────────────┐   ┌──────────────────────────┐
│ MySQL 8.4 (3306) │   │ Python Agent (8000 内网)  │
│ 业务库/工单/消息  │   │ LangGraph：智能对话/RAG/  │
│ /租户/审计/归档   │   │ 工具调用/人工接管中断     │
└──────────────────┘   └──────┬─────────┬─────────┘
                              ▼         ▼
                     PostgreSQL (5432)  Qdrant (6333)
                     会话/Prompt/用量   知识库向量
┌──────────────────────────────────────────────────┐
│ Redis 7（内网 6379，强制依赖）：登录防爆破/黑名单/ │
│ 模型配置失效广播/知识库索引队列/定时任务锁          │
└──────────────────────────────────────────────────┘
┌──────────────────────────────────────────────────┐
│ 可观测栈：Grafana(3000)/Prometheus(9090)/          │
│          Loki(3100)/Jaeger(16686)                 │
└──────────────────────────────────────────────────┘
```

## 🔌 服务端口

| 服务 | 端口 | 说明 |
|------|------|------|
| Nginx | 8080 | 前端 + API 入口 |
| MySQL | 3306 | 业务库（开发默认 root/123456，生产请修改 .env） |
| PostgreSQL | 5432 | Agent 会话 checkpoint |
| Qdrant | 6333 | 向量数据库 |
| Redis | 内网 6379 | 登录限流 / Token 黑名单 / 模型配置广播 / 定时任务锁 |
| Grafana | 3000 | 监控可视化 |
| Prometheus | 9090 | 指标查询 |
| Loki | 3100 | 日志查询 |
| Jaeger | 16686 | 链路追踪 UI |

## 📁 目录结构

```
customer_service_prod/
├── docker-compose.yaml       # 一键编排（全部服务）
├── .env.example              # 环境变量模板
├── .gitlab-ci.yml            # CI/CD 流水线（lint/test/build/deploy）
├── go_service/               # Go 网关（internal/：handler/service/model/repository/middleware/config/pkg）
├── python_agent/             # Python LangGraph Agent（main/db_ops/agent_factory/routers）
├── admin-app/                # 管理后台前端（Vue3 + Vite，nginx 镜像构建时容器内自动编译）
├── web/                      # 前端构建产物 + 静态资源
│   ├── admin/                #   管理后台（平台/租户双端）
│   ├── chat/                 #   用户端 pc.html / h5.html
│   └── sdk/                  #   cs-chat.js SDK + 多端接入示例
├── deploy/
│   ├── nginx.conf            #   Nginx 配置
│   ├── cs_images/            #   文档用操作截图
│   ├── init/                 #   初始化 SQL（mysql/、postgres/）
│   └── deploy.sh / push-to-acr.sh  # 基础部署 / 镜像上传脚本
│   # （production/、backup/、observability/ 属商业版内容，不随开源仓库发布，见 COMMERCIAL.md）
├── docs/                     # 文档中心（见下方导航）
├── scripts/                  # 验证/构建/运维脚本
└── thirdDemo/                # 第三方接入最小示例（Go + SDK）
```

## 📚 文档导航

| 文档 | 内容 | 适用 |
|------|------|------|
| [📖 图文操作手册](./docs/README.md) | **10 张截图逐步讲解每个模块怎么操作** | 所有人 |
| [⚡ 快速体验](./docs/快速体验.md) | 5 分钟跑通部署→配置→对话→转人工 | 所有人 |
| [产品设计文档](./docs/产品设计文档.md) | 产品定位、系统架构、多租户设计、安全设计 | 产品/架构师 |
| [产品使用说明文档](./docs/产品使用说明文档.md) | 全角色文字版使用指南、第三方接入、运维 | 用户/运维 |
| [模块功能说明文档](./docs/模块功能说明文档.md) | 模块详细功能、数据模型、流程设计 | 开发/测试 |
| [架构与开发指南](./docs/05-架构与开发指南.md) | 目录结构、模块职责、开发规范 | 开发者 |
| [运维监控指南](./docs/运维监控指南.md) | Grafana/Prometheus/Loki/Jaeger 使用、故障排查 | 运维/SRE |
| [客户接入手册](./docs/接入手册.md) | 产品化接入步骤、Token 换取、多端接入 | 客户/第三方 |
| [CI/CD 流水线说明](./docs/CICD流水线使用说明.md) | 流水线架构、部署脚本联动 | 运维/DevOps |
| [ACR 镜像上传说明](./docs/阿里云ACR镜像上传说明.md) | 镜像打包上传、版本管理 | DevOps |

## ❓ 常见问题

**Q：接口返回 429？**
防爆破锁定（同一 IP+key 连续失败 5 次锁 10 分钟）。`docker restart cs_go` 可清锁。

**Q：AI 没反应？**
检查「平台模型配置」：对话模型/向量模型是否配置、API Key 是否正确、测试连通性是否通过。

**Q：修改了代码如何生效？**
后端 Go：`docker compose up -d --build go`；前端（admin-app / chat / sdk）：`docker compose up -d --build nginx`（nginx.Dockerfile 容器内自动编译管理后台，无需宿主机安装 Node）；nginx.conf：改后 `docker exec cs_nginx nginx -s reload`。

**Q：知识库检索不到？**
文档状态需为「已索引」；更换 Embedding 模型后必须到知识库页重新索引。

**Q：脚本在 WSL 执行报错？**
仓库脚本为 LF 行尾；若从 Windows 检出为 CRLF，先 `sed -i 's/\r$//' scripts/*.sh`。

---

<div align="center">

**© 2026 智能客服系统** · [AGPL-3.0](./LICENSE) · [商用授权](./COMMERCIAL.md) · 技术支持：2771452556@qq.com

</div>
