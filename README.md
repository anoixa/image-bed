# image-bed

一个基于 Go + Gin 的轻量级图床服务，支持多种存储后端、图片转换和相册管理。

## 功能特性

- **多存储后端支持**：本地磁盘、MinIO/S3、WebDAV
- **图片处理**：基于 libvips 的 WebP/AVIF 自动转换、缩略图生成
- **相册管理**：创建相册、批量管理图片
- **多种认证方式**：JWT 认证、API Token、Refresh Token、OAuth 登录（GitHub / Google / Gitee）
- **缓存支持**：内存缓存 (Ristretto) 或 Redis
- **限流保护**：基于令牌桶的 API 和图片访问限流
- **数据统计**：Dashboard 统计面板

## 技术栈

- **后端**: Go 1.26 + Gin
- **数据库**: SQLite (默认) / PostgreSQL
- **图片处理**: libvips (govips)
- **缓存**: Ristretto (内存) / Redis
- **文档**: Swagger

## 构建

### 依赖

- Go 1.26+
- libvips-dev (图片处理库)

**安装 libvips:**

```bash
# Ubuntu/Debian
sudo apt-get install libvips-dev

# macOS
brew install vips

# Arch Linux
sudo pacman -S libvips
```

### 构建命令

```bash
# 克隆仓库
git clone https://github.com/anoixa/image-bed.git
cd image-bed

# 安装依赖
go mod download

# 复制配置文件
cp .env.example .env

# 构建（开发模式）
go build -o image-bed .

# 构建（生产模式，带版本信息）
CGO_ENABLED=1 go build -ldflags="-s -w \
  -X github.com/anoixa/image-bed/config.Version=release \
  -X github.com/anoixa/image-bed/config.CommitHash=$(git rev-parse --short HEAD)" \
  -o image-bed .

# 运行
./image-bed serve
```

### Docker 构建

```bash
docker compose up -d --build
```

默认使用 SQLite 和 Docker named volume，未提供 `.env` 也可以启动。首次启动会自动创建管理员账号，密码查看容器日志。

如使用已发布镜像，可直接运行：

```bash
docker run -d \
  --name image-bed \
  -p 8080:8080 \
  -v image-bed:/app/data \
  <image-name>:<tag>
```

如果改用宿主机目录 bind mount，需确保目录 owner 可写，而不是使用 `chmod 777`：

```bash
mkdir -p ./data
sudo chown -R "$(id -u):$(id -g)" ./data
PUID="$(id -u)" PGID="$(id -g)" docker compose up -d --build
```

## 前端集成

本项目为纯后端 API 服务，前端需单独部署。

### 前端项目

- **仓库**: https://github.com/anoixa/image-bed-web
- **技术栈**: React + TypeScript + Vite

### 集成步骤

1. **克隆并构建前端**

```bash
git clone https://github.com/anoixa/image-bed-web.git
cd image-bed-web
npm install
npm run build
```

2. **放置前端文件**

将前端构建产物放入本项目的 `public/dist/` 目录：

```bash
# 假设前端项目在同级目录
cp -r ../image-bed-web/dist/* ./public/dist/
```

3. **配置后端**

确保 `.env` 中启用前端服务：

```env
SERVE_FRONTEND=true
```

4. **访问**

启动后端服务后，访问 `http://localhost:8080` 即可看到前端界面。

### 仅使用 API

如不需要前端界面，设置 `SERVE_FRONTEND=false`，后端仅提供 API 服务。

## API 文档

启动服务后访问：`http://localhost:8080/swagger/index.html`

## OAuth 登录

支持 GitHub、Google、Gitee OAuth2 登录。

项目不提供公开注册。账号只能由管理员手动创建；用户登录后可在账号设置中自行绑定 OAuth2 账号。未绑定的 OAuth2 账号不能直接登录，也不会自动创建用户。

启用 OAuth2 时需将 `SERVER_DOMAIN` 配置为浏览器可访问的公网地址，并在 provider 控制台配置 callback URL：

```text
<SERVER_DOMAIN>/api/auth/oauth/<provider>/callback
```

## 图片格式自动协商

图片访问接口会根据客户端 `Accept` 头自动选择原图、WebP 或 AVIF 变体，客户端无需拼接不同格式的图片地址。

## 配置

主要配置项（`.env` 文件）：

```env
# 服务器
SERVER_HOST=127.0.0.1
SERVER_PORT=8080
SERVER_DOMAIN=http://localhost:8080

# 数据库 (sqlite 或 postgresql)
DB_TYPE=sqlite

# 缓存 (memory 或 redis)
CACHE_TYPE=memory

# JWT
JWT_SECRET=
JWT_ACCESS_TOKEN_TTL=15m
JWT_REFRESH_TOKEN_TTL=168h

# 认证 / OAuth
AUTH_PASSWORD_LOGIN_ENABLED=true

# Worker
WORKER_COUNT=-1
WORKER_MEMORY_LIMIT_MB=512
```

`JWT_SECRET` 留空时会在首次启动自动生成并持久化到 `data/jwt.secret`；生产环境也可以手动指定至少 32 字符的强密钥。`SERVER_DOMAIN` 会影响图片链接和 OAuth callback URL。`CORS_ORIGINS` 主要用于纯 API / 前后端分离部署，嵌入前端同源访问通常无需配置。

完整配置见 `.env.example`

## 默认账号

首次启动会自动创建管理员账号：

- 用户名: `admin`
- 密码: 随机生成（查看启动日志获取）

**注意**: 登录后请立即修改默认密码！

## 命令行工具

```bash
# 数据库备份
./image-bed backup
./image-bed backup --output ./backups/my-backup.tar.gz

# 清理缓存
./image-bed cache clear
./image-bed cache clear --all

# 数据库迁移
./image-bed migrate
```

## 许可证

MIT
