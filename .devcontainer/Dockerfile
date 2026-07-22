FROM mcr.microsoft.com/devcontainers/base:bullseye

USER root

# 换国内源
RUN sed -i 's@http://deb.debian.org@https://mirrors.aliyun.com@g' /etc/apt/sources.list && \
    sed -i 's@http://security.debian.org@https://mirrors.aliyun.com/debian-security@g' /etc/apt/sources.list && \
    apt-get clean && \
    apt-get update -y

# 安装必要工具
RUN apt-get install -y --no-install-recommends \
    curl \
    wget \
    git \
    jq \
    vim \
    conntrack \
    && rm -rf /var/lib/apt/lists/*
