# ============================================================================
# Nginx 多阶段构建：容器内编译 admin 前端 + 打包静态资源
#
# 为什么需要它：
#   - web/admin/dist 是 Vite 构建产物，不随仓库发布（.gitignore 排除 dist/）
#   - 传统方式需先在宿主机执行 scripts/build_admin_app.sh（依赖 WSL + 宿主机 Node）
#   - 本 Dockerfile 把构建步骤内置进镜像，docker compose up -d --build 即一键启动，
#     无需在宿主机安装 Node / 预先构建前端
#
# 构建命令：docker compose up -d --build nginx
# ============================================================================

# ---------- 阶段 1：构建 admin-app 前端（Node） ----------
FROM node:20-alpine AS admin-builder
WORKDIR /build/admin-app
# 先只拷贝依赖清单，利用 Docker 层缓存加速重复构建
COPY admin-app/package.json admin-app/package-lock.json ./
RUN npm ci --registry=https://registry.npmmirror.com --no-audit --no-fund
# 拷贝源码并构建（vite.config.js outDir='../web/admin/dist' → 产物在 /build/web/admin/dist）
COPY admin-app/ ./
RUN npm run build

# ---------- 阶段 2：Nginx 运行镜像 ----------
FROM nginx:alpine
# 站点配置（compose 仍保留挂载可覆盖，便于运维调整后 reload）
COPY deploy/nginx.conf /etc/nginx/conf.d/default.conf
# 静态资源：chat 内核 / sdk 接入示例 / 其他静态文件
COPY web/ /usr/share/nginx/html/web/
# 管理后台构建产物 → /web/admin/dist/（nginx.conf /admin/ alias 指向该目录；
# 若本地存在旧 dist 会被本层覆盖，保证镜像内始终是最新构建产物）
COPY --from=admin-builder /build/web/admin/dist/ /usr/share/nginx/html/web/admin/dist/
EXPOSE 80
