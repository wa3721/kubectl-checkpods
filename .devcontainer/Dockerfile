FROM mcr.microsoft.com/devcontainers/base:bullseye

# 换为 HTTPS + 国内镜像（阿里云）
RUN sed -i \
    's@http://deb.debian.org@https://mirrors.aliyun.com@g' \
    /etc/apt/sources.list && \
    sed -i \
    's@http://security.debian.org@https://mirrors.aliyun.com/debian-security@g' \
    /etc/apt/sources.list && \
    apt-get clean && \
    apt-get update -y