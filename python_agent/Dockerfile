# -------------------------- 构建阶段 builder --------------------------
FROM python:3.13-slim AS builder

ENV PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    TZ=Asia/Shanghai

WORKDIR /build

# 仅builder安装tzdata，这里现在已经不需要复制时区文件到运行阶段，如果你后续想保留复制时区逻辑可以保留这行
RUN apt-get update && apt-get install -y --no-install-recommends tzdata \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
# 安装依赖到独立目录
RUN pip install --no-cache-dir --prefix=/install -r requirements.txt

COPY . .

# -------------------------- 运行阶段 runtime（最终镜像） --------------------------
FROM python:3.13-slim

ENV PIP_INDEX_URL=https://pypi.tuna.tsinghua.edu.cn/simple \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    TZ=Asia/Shanghai

WORKDIR /app

# 复制python依赖包
COPY --from=builder /install /usr/local
# 复制项目源码
COPY --from=builder /build /app

EXPOSE 8000
CMD ["python","main.py"]