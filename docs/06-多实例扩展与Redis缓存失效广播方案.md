# 06 · 多实例扩展与 Redis 缓存失效广播方案

> 状态：**部分实施**（阶段 0-1 已落地：强制 Redis 广播 + 签名兜底；阶段 2 多实例扩容、Redis 高可用未实施）
> 适用范围：Agent 服务水平扩展
> 关联文档：05-架构与开发指南.md、运维监控指南.md

---

## 1. 背景与动机

### 1.1 为什么现在不需要
当前 Agent 服务为 **单进程 asyncio**（uvicorn 1 worker），属于 **IO 密集型**服务（等待 LLM API / Postgres / Qdrant 响应），单进程已能支撑数百路并发聊天。此阶段**不建议**盲目引入多 worker 或多实例。

### 1.2 什么时候需要
满足以下任一条件时启动本方案：

| 触发条件 | 说明 |
|---|---|
| 并发聊天量级大幅上升 | 单进程 CPU/事件循环成为瓶颈（如本地 embedding 推理吃 CPU） |
| 需要高可用（HA） | 单实例故障即全站不可用，需要多副本容灾 |
| 本地模型部署 | BGE 等本地 embedding / 本地 LLM 推理为 CPU/GPU 密集，多实例并行可摊薄算力 |
| 租户量级扩张 | 单实例内存中缓存（模型实例 / Agent 图 / 检索器）占用过大 |

### 1.3 不选多 worker（单机多进程）的原因
- `uvicorn --workers N` 各 worker 进程内缓存独立，失效需广播，与多实例完全相同，却没有跨机容灾能力；
- 多实例可直接利用 Docker Compose scale / K8s，运维模型更统一；
- 因此本方案直接设计为 **多实例（水平扩展）**，单机多 worker 是它的退化特例。

---

## 2. 现状与瓶颈盘点

### 2.1 当前进程内状态（Agent 单实例）

> **已实施（2026-09-10）**：缓存失效已改为 **Redis 广播单通道**（强制依赖，无 HTTP 直调）；
> 并配套「配置签名对比」兜底（广播丢失时 ≤10s 最终一致）。多实例化所需的进程内状态改造已就绪。

| 状态 | 内容 | 失效方式 |
|---|---|---|
| `_tenant_model_cache` | 租户模型配置 + 对话模型实例（`_model_*`）+ embedding 实例（`_emb_*`） | Redis 广播 `cs:model:cache:invalidate`（scope 粒度）+ 5min TTL 兜底 |
| `app.state.tenant_agents` | 租户 LangGraph Agent 图（绑定模型实例 + retriever 引用） | Redis 广播消费后按 scope pop |
| `app.state.tenant_retrievers` | 租户向量检索器（绑定 embedding 实例） | Redis 广播消费后按 scope pop |
| `app.state.checkpointer` | AsyncPostgresSaver（已加锁，串行写 checkpoint） | 每实例独立连接，无需广播 |
| `_agent_sigs`（2026-09-10 新增） | 租户最近构建的模型配置签名（config_version） | 每次构建 Agent 前懒检查（10s TTL），变化即重建——广播丢失兜底 |

### 2.2 多实例化后暴露的问题
1. **缓存失效只作用于被调用的实例**：HTTP 直调 `/agent/cache/clear` 只清当前请求打到的实例，其余实例保留旧配置 → 聊天仍走旧模型（用户切换模型"部分生效"）。
2. **定时任务重复执行**：`cleanup_sessions`（过期会话回收）如果每个实例都跑，会并发删除同一批数据（有 `adelete_thread` 幂等保护，但仍是浪费与风险）。
3. **Go → Agent 请求分发**：当前 `AgentURL` 为单地址 `agent:8000`，多实例后需负载均衡。
4. **Go 网关本身**：`WSHub`（会话并发锁/配额）为 Go 进程内状态。本方案**暂不扩展 Go**（网关保持单实例，瓶颈在 Agent 侧），避免引入分布式锁的复杂度。

---

## 3. 目标架构

```
                        ┌────────────────────────────────────────┐
                        │              Go 网关 (单实例)           │
                        │  鉴权 / WS / 配额 / 配置保存 / 定时任务    │
                        └───────┬────────────────┬───────────────┘
                                │ HTTP 分发(轮询) │ Redis PUBLISH(失效广播)
                        ┌───────▼───────┐ ┌──────▼───────────────┐
                        │  nginx/Go轮询  │ │        Redis         │
                        │ (agent 集群)   │ │ channel: cs:model:cache:invalidate
                        └───────┬───────┘ └──────▲───────────────┘
              ┌─────────────────┼────────────────┼──────────────┐
        ┌─────▼─────┐     ┌─────▼─────┐     ┌─────▼─────┐
        │ agent-1   │     │ agent-2   │     │ agent-N   │  ← 每实例订阅 Redis
        │ (订阅)    │     │ (订阅)    │     │ (订阅)    │     收到消息→清本地缓存
        └─────┬─────┘     └─────┬─────┘     └─────┬─────┘
              └─────────┴────────┴───────┘
                   Postgres(共享) / Qdrant(共享) / MySQL(Go侧)
```

要点：
- **Agent 无状态化**（业务数据都在共享存储：Postgres / Qdrant / MySQL），进程内只有可重建缓存；
- **Redis 只承担两件事**：缓存失效广播（pub/sub）+ 定时任务分布式锁（SETNX）；
- **Go 网关保持单实例**，负责请求分发与配置保存发布。

---

## 4. 核心设计：Redis 缓存失效广播

### 4.1 消息协议

**Channel**：`cs:model:cache:invalidate`

**消息体**（JSON，UTF-8）：

```json
{
  "tenant_id": "t_demo",
  "scope": "chat",
  "ts": 1789000000
}
```

| 字段 | 说明 |
|---|---|
| `tenant_id` | 空字符串 = 清全部（平台默认配置变更）；非空 = 只清该租户 |
| `scope` | `chat` / `embedding` / `all`（语义与现有 `/agent/cache/clear` 完全一致） |
| `ts` | 发布时间戳，用于接收方日志排查与重复消息去重（可选） |

**已实施（2026-09-10）**：强制 Redis 广播单通道，HTTP 直调已移除（单实例自订阅自消费，多实例全量消费，一套逻辑）。

### 4.2 发布端（Go 网关）

**已实施（2026-09-10）**：三个配置保存 handler（`model.go` / `tenant.go` / `platform.go`）保存成功后
仅执行 `cache.Publish("cs:model:cache:invalidate", {tenant_id, scope, ts})`，**HTTP 直调已移除**；发布失败仅记日志，不影响保存主流程。

```
PUBLISH cs:model:cache:invalidate {"tenant_id":"t_demo","scope":"chat","ts":...}
```

- `scope` 沿用已实现的 `service.ModelUpdateScope(updates)` 判断；
- 平台默认配置（platform.go）发布 `tenant_id=""`（所有实例清全部）；
- Agent 侧 `/agent/cache/clear` HTTP 入口保留，仅作手工调试。

### 4.3 订阅端（Agent 实例）

**已实施（2026-09-10）**：实现落在 `redis_pubsub.py`（`invalidate_listener`：订阅 + 断线 3s 重连重订阅），
启动前 `ping()` 失败即启动失败（fail-fast）；清缓存逻辑抽为公共函数 `clear_tenant_caches(app, tenant_id, scope)`，
HTTP 入口与订阅入口复用同一实现，保证行为一致。

**广播丢失兜底（2026-09-10 新增，配置签名对比）**：pub/sub 无持久化，广播在断线/实例重启窗口丢失时——

```python
# agent_factory.get_tenant_agent 每次构建前懒检查（_SIG_CHECK_TTL=10s）
remote = await _fetch_config_version(app, tenant_id)   # GET /api/agent/model-config → config_version
if remote and remote != _agent_sigs.get(tenant_id):
    clear_model_cache(tenant_id, "all")                # 清配置缓存
    agents.pop(tenant_id, None); retr.pop(tenant_id, None)
    _agent_sigs[tenant_id] = remote                    # 重建后记录新签名
```

- `config_version` = Go 端对 chat+embedding 全部相关字段（含明文 key）的 sha256 前 16 位；
- **广播负责即时，签名负责最终一致**（最多延迟 10s）；检查失败（Go 不可达）不阻断聊天；
- 构建 Agent 后从配置缓存补记签名，保证广播路径与签名路径一致。

### 4.4 定时任务分布式锁

**已实施（2026-09-10）**：`cleanup_sessions`（过期会话回收）使用 Redis SETNX 抢锁，锁不可用时跳过本轮（fail-closed）：

---

## 5. 代码改动清单

### 5.1 Agent 侧（python_agent/）
| 文件 | 改动 | 状态 |
|---|---|---|
| `main.py` | lifespan 内启动 Redis 订阅任务；注入 `redis` 到 `app.state`；启动前 ping 失败即失败 | ✅ 已实施 |
| `routers_cache.py` | 抽公共函数 `clear_tenant_caches(app, tenant_id, scope)`；HTTP 入口改为调用它 | ✅ 已实施 |
| `agent_factory.py` | 新增 `_agent_sigs` 签名懒检查（10s TTL），广播丢失兜底重建 | ✅ 已实施 |
| `db_ops.py` | `cleanup_sessions` 调用处加 Redis 分布式锁（SETNX，锁不可用跳过本轮） | ✅ 已实施 |
| `requirements.txt` | 新增 `redis==5.2.1` | ✅ 已实施 |
| 新增 `redis_pubsub.py` | 订阅任务 + 消息解析 + 断线重连 | ✅ 已实施 |

### 5.2 Go 侧（go_service/）
| 文件 | 改动 | 状态 |
|---|---|---|
| `internal/pkg/agent/client.go` | 新增 `AgentURLs` 轮询支持（`AGENT_URLS` 逗号分隔，round-robin）；现有单地址逻辑保留为兼容 | ⬜ 未实施（阶段 2） |
| `internal/config/config.go` | 读取 `AGENT_URLS`、`REDIS_ADDR` | ⬜ 部分（REDIS_ADDR 已实施，AGENT_URLS 未） |
| `internal/handler/model.go` / `tenant.go` / `platform.go` | 配置保存成功后 `PUBLISH` 失效消息（HTTP 直调已移除） | ✅ 已实施 |
| `internal/pkg/cache/redis.go`（新增） | go-redis 封装；`Init()` Ping 失败返回错误（fail-fast） | ✅ 已实施 |
| `internal/service/model_service.go` | 新增 `config_version`（sha256 前 16 位）供 Agent 签名兜底 | ✅ 已实施 |

### 5.3 部署（docker-compose.yaml）
| 服务 | 改动 | 状态 |
|---|---|---|
| `redis` | 新增（`redis:7-alpine`，AOF + LRU 256MB + 健康检查，仅内网） | ✅ 已实施（根 compose + deploy/production 均含） |
| `agent` | 增加 `REDIS_URL` 环境变量 + depends_on redis | ✅ 已实施；`--scale agent=3` ⬜ 未实施 |
| `nginx` 或 Go | Agent 请求分发改为多实例（见 5.2 轮询，或 nginx upstream） | ⬜ 未实施（阶段 2） |

### 5.4 环境变量
```
# Go（REDIS_ADDR 已实施；AGENT_URLS 阶段 2 启用）
AGENT_URLS=http://agent:8000,http://agent2:8000,http://agent3:8000   # ⬜ 阶段 2
REDIS_ADDR=redis:6379                                                # ✅ 已实施

# Agent
REDIS_URL=redis://redis:6379/0                                       # ✅ 已实施
ENABLE_SESSION_CLEANUP=true   # 仅一个实例置 true（或全部 true + Redis 锁兜底）✅
```

---

## 6. 分阶段实施步骤

### 阶段 0：准备（半天）✅ 已实施
1. ✅ 抽公共清缓存函数 `clear_tenant_caches`，HTTP 与订阅复用（行为零变化，回归验证）。
2. ✅ compose 增加 `redis` 服务（仅内网，不映射宿主端口；根 compose 与 deploy/production 均已含）。

### 阶段 1：单实例 + Redis 广播（1 天）✅ 已实施
1. ✅ Agent 增加 Redis 订阅任务（不开启多实例，订阅即生效）。
2. ✅ Go 配置保存 → **Redis 发布广播**（`cs:model:cache:invalidate`，租户+scope 粒度）。
3. ✅ 验证：保存租户模型配置 → agent 日志出现订阅消费记录，缓存按 scope 清除。
4. ✅（增强）强制 Redis 改造：启动 fail-fast、运行期 fail-closed、移除 HTTP 双通道（commit `5859428`）。
5. ✅（增强）配置签名对比兜底：广播丢失 ≤10s 最终一致（commit `9565b06`）。

### 阶段 2：多实例扩容（1 天）⬜ 未实施
1. Go 支持 `AGENT_URLS` 轮询；compose `--scale agent=3`（或显式 agent1/2/3 服务）。
   - **注意会话亲和**：`hash(thread_id) % N`，同 thread 恒定同实例（进程内缓存 + 线程锁是进程级的）。
2. 会话清理加 Redis 分布式锁。✅ 锁已实施，多实例可直接用。
3. 验证：三实例下切换模型 → 三实例日志都显示清除；聊天打到任一实例均用新模型。

### 阶段 3：收尾（半天）⬜ 未实施
1. ✅ 已移除 HTTP 直调（强制 Redis 广播单通道；agent 保留 `/agent/cache/clear` 仅作手工调试入口）。
2. ⬜ 更新运维监控指南（Redis 指标、agent 实例数、日志关键字）——Redis 监控章节已补，agent 实例数待多实例后补。
3. ⬜ 压测：三实例并发 chat 吞吐与 P99 对比单实例。

> **Redis 高可用（2026-09-10 新增，未实施）**：强制依赖 Redis 后其为单点（宕机=登录不可用）。
> 方案见 §9：生产推荐云 Redis（高可用版）或 compose 主从+Sentinel（1 主 1 从 + 3 哨兵）。
> 建议在阶段 2 之前实施（先消除基础设施单点，再扩展应用实例）。

---

## 7. 验证方案

| 用例 | 步骤 | 预期 | 状态 |
|---|---|---|---|
| 广播失效（单实例） | 运行中保存 t_demo 对话模型配置 | agent 日志出现 `tenant=t_demo scope=chat` 清除记录 | ✅ 已验（2 次保存=2 条消费） |
| 切模型即时生效 | 切模型后发消息 | 新模型回复（agent 日志显示新模型构建） | ✅ 已验 |
| 广播丢失兜底 | 重启 agent 窗口内改配置（订阅断开） | 发消息 → 日志「配置签名变化 …（广播兜底，重建 Agent）」→ 新模型回复 | ✅ 已验（commit `9565b06`） |
| fail-fast | Redis 不可用时启动 go/agent | 启动失败、健康检查 unhealthy | ✅ 已验 |
| fail-closed | Redis 运行中停止 | 登录 429、登出 token 按已拉黑 | ✅ 已验 |
| scope 粒度 | 只改向量模型 | 只清 `_emb_*` + 检索器 + Agent，不清 `_model_*` | ✅ 已验 |
| 定时任务互斥 | 多实例同时到点 | 仅一个实例执行 cleanup（Redis 锁日志） | ✅ 单实例已验；多实例待阶段 2 |
| 广播失效（多实例） | 三实例运行中保存配置 | 三个实例日志均出现清除记录 | ⬜ 待阶段 2 |
| 单实例故障 | kill 一个 agent 副本 | 其余实例正常服务；新配置仍全量广播 | ⬜ 待阶段 2 |

---

## 8. 风险与回滚

> **实施状态（2026-09-10 更新）**：阶段 0-1 已落地并**强制依赖 Redis**——
> ① 启动 fail-fast（Redis 不可用启动失败）；② 运行期查询失败 fail-closed
> （登录限流拒绝、黑名单按命中处理）；③ 缓存失效只保留 Redis 广播单通道，
> HTTP 直调已移除。Redis 与 MySQL/Postgres/Qdrant 同为硬基础设施。

| 风险 | 缓解 |
|---|---|
| Redis 不可用（运行中宕机） | fail-closed：登录被拒（429）、登出 token 按已拉黑处理；服务明确失败而非静默降级。**生产必须配套 Redis 高可用**：compose 主从+Sentinel 或云 Redis，否则单点故障=登录不可用 |
| Redis 不可用（部署时） | 启动 fail-fast，容器健康检查暴露，部署环节即失败，不进入运行期 |
| 广播消息丢失（Redis pub/sub 无持久化） | **已缓解（2026-09-10）**：① 配置缓存 TTL 300s 兜底（延迟生效）；② 配置签名对比兜底——每次构建 Agent 前懒检查 `config_version`（10s TTL），变化即重建，**已构建的 Agent/检索器不再受广播丢失影响**，≤10s 最终一致 |
| 广播丢失窗口的限流/黑名单 | fail-closed 拒绝（明确失败）而非放行；Redis 恢复后自动恢复 |
| 回滚 | 回退到 `a607964`（含双通道版本）或 `aa30297`（内存降级前）均可，git 分支可追溯 |

---

## 9. 边界与暂不实施项

| 项 | 说明 |
|---|---|
| **Redis 高可用（2026-09-10 新增，未实施）** | 强制依赖 Redis 后其为单点。两个方向：① compose 主从+Sentinel（1 主 1 从 + 3 哨兵，客户端改 FailoverClient，切换窗口 ~10-30s）；② 生产直接上云 Redis（阿里云/腾讯云高可用版，自带监控告警）。**推荐 ②**（多租户生产）；① 仅在客户要求私有化 HA 时做。单机 WSL 部署 ① 只能演练切换，机器整体故障仍全挂 |
| Go 网关多实例 | 需引入分布式会话锁（WSHub 跨实例），本方案范围外；当前 Go 无状态程度高（MySQL 共享），后续可独立立项 |
| Redis Streams 持久化广播 | pub/sub + 签名兜底已满足"即时通知 + 最终一致"，暂不引入消费组复杂度 |
| Agent 实例优雅下线 | scale down 时 in-flight 流式请求会中断，后续可加 `/agent/health` 摘流 + 排水 |
| 本地 embedding 推理服务 | 与本方案正交（text-embeddings-inference 独立服务），但多实例会放大本地算力价值，建议搭配部署 |

---

*文档维护：随实施进度更新状态与验证结果。*
