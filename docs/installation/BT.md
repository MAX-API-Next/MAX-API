# 宝塔面板部署教程

本文档提供使用宝塔面板 Docker 功能部署 MAX API 的图文教程。

> 📖 官方文档：[宝塔面板部署](https://github.com/MAX-API-Next/MAX-API)

***

## 前置要求

| 项目    | 要求                                 |
| ----- | ---------------------------------- |
| 宝塔面板  | ≥ 9.2.0 版本                         |
| 推荐系统  | CentOS 7+、Ubuntu 18.04+、Debian 10+ |
| 服务器配置 | 至少 1 核 2G 内存                       |

***

## 步骤一：安装宝塔面板

1. 前往 [宝塔面板官网](https://www.bt.cn/new/download.html) 下载适合您系统的安装脚本
2. 运行安装脚本安装宝塔面板
3. 安装完成后，使用提供的地址、用户名和密码登录宝塔面板

***

## 步骤二：安装 Docker

1. 登录宝塔面板后，在左侧菜单栏找到并点击 **Docker**
2. 首次进入会提示安装 Docker 服务，点击 **立即安装**
3. 按照提示完成 Docker 服务的安装

***

## 步骤三：安装 MAX API

### 方法一：使用宝塔应用商店（推荐）

1. 在宝塔面板 Docker 功能中，点击 **应用商店**
2. 搜索并找到 **MAX-API**
3. 点击 **安装**
4. 配置以下基本选项：
   - **容器名称**：可自定义，默认为 `max-api`
   - **端口映射**：设置为 `127.0.0.1:3000:3000`，仅允许服务器本机和反向代理访问
   - **环境变量**：
     - `SESSION_SECRET`：会话密钥（**必填**，多机部署时必须一致）
     - `CRYPTO_SECRET`：可选的加密密钥覆盖项；未设置时回退使用 `SESSION_SECRET`，如单独设置则多机/Redis 节点必须一致
5. 点击 **确认** 开始安装
6. 等待安装完成后，为站点配置 HTTPS 反向代理，再通过 `https://您的域名` 登录；`http://127.0.0.1:3000` 仅用于服务器本机检查，不应直接暴露到公网

### 方法二：使用 Docker Compose

1. 在宝塔面板中创建网站目录，如 `/www/wwwroot/max-api`
2. 创建 `docker-compose.yml` 文件：

```yaml
version: '3'
services:
  max-api:
    image: cscitechtop/max-api:latest@sha256:006d5d86887a261baab4d71ec3797d429e3771a4836e5899734aee0e7f66f2ab
    container_name: max-api
    restart: always
    ports:
      - "127.0.0.1:3000:3000"
    volumes:
      - ./data:/data
    environment:
      - SESSION_SECRET=your_session_secret_here  # 请修改为随机字符串
      - TZ=Asia/Shanghai
```

1. 在终端中进入目录并启动：

```bash
cd /www/wwwroot/max-api
docker-compose up -d
```

***

## 配置说明

### 必要环境变量

| 变量名                 | 说明                 | 是否必填   |
| ------------------- | ------------------ | ------ |
| `SESSION_SECRET`    | 会话密钥，多机部署必须一致      | **必填** |
| `CRYPTO_SECRET`     | 可选的加密密钥覆盖项；未设置时回退使用 `SESSION_SECRET`，显式设置时多机/Redis 节点需一致 | 可选 |
| `SQL_DSN`           | 数据库连接字符串（使用外部数据库时） | 可选     |
| `REDIS_CONN_STRING` | Redis 连接字符串        | 可选     |

### 生成随机密钥

```bash
# 生成 SESSION_SECRET
openssl rand -hex 16

# 或使用 Linux 命令
head -c 16 /dev/urandom | xxd -p
```

***

## 常见问题

### Q1：无法通过 HTTPS 域名访问？

1. 在服务器本机执行 `curl http://127.0.0.1:3000`，确认 MAX API 容器正常响应
2. 检查宝塔站点的 HTTPS 反向代理目标是否为 `http://127.0.0.1:3000`
3. 检查域名解析、TLS 证书和 443 端口；不要在防火墙或云安全组中向公网开放 3000 端口

### Q2：登录后提示会话失效？

确保设置了 `SESSION_SECRET` 环境变量，且值不为空。

### Q3：数据如何持久化？

使用 Docker 卷映射数据目录：

```yaml
volumes:
  - ./data:/data
```

### Q4：如何更新版本？

```bash
# 拉取经过校验的固定镜像
docker pull cscitechtop/max-api:latest@sha256:006d5d86887a261baab4d71ec3797d429e3771a4836e5899734aee0e7f66f2ab

# 重启容器
docker-compose down && docker-compose up -d
```

***

## 相关链接

- [官方文档](https://github.com/MAX-API-Next/MAX-API)
- [环境变量配置](https://github.com/MAX-API-Next/MAX-API)
- [常见问题](https://github.com/MAX-API-Next/MAX-API)
- [GitHub 仓库](https://github.com/MAX-API-Next/MAX-API)

***

## 截图示例

![宝塔面板 Docker 安装](https://github.com/user-attachments/assets/7a6fc03e-c457-45e4-b8f9-184508fc26b0)

> ⚠️ 注意：密钥为环境变量 `SESSION_SECRET`，请务必设置！
