# Public 静态文件目录

本目录用于存放 Vite + React 等前端框架编译后的静态文件。

## 目录结构

```
public/
├── dist/           # 前端构建输出目录（需要手动放入或使用构建脚本）
│   ├── index.html
│   ├── assets/
│   │   ├── index-xxx.js
│   │   └── index-xxx.css
│   └── ...
├── fs_default.go   # 生产模式：使用 go:embed 嵌入文件
├── fs_dev.go       # 开发模式：直接从磁盘读取（支持热重载）
└── public.go       # 静态文件服务 Handler

```

## 使用方法

### 1. 构建前端应用

```bash
cd your-frontend-project
npm run build
```

### 2. 复制构建产物

```bash
# 方法 1: 直接复制
cp -r your-frontend-project/dist/* /path/to/image-bed/public/dist/

# 方法 2: 使用符号链接（开发时推荐）
ln -s /path/to/your-frontend-project/dist /path/to/image-bed/public/dist
```

### 3. 构建 Go 应用

**生产模式**（嵌入静态文件，单二进制文件部署）：
```bash
go build -o image-bed .
```

**开发模式**（从磁盘读取，支持热重载）：
```bash
go build -tags=dev -o image-bed .
```

## 开发模式 vs 生产模式

| 特性 | 生产模式 (默认) | 开发模式 (`-tags=dev`) |
|------|----------------|------------------------|
| 静态文件位置 | 嵌入在二进制中 | 从 `public/dist` 读取 |
| 文件修改后重编译 | 需要 | 不需要 |
| 单文件部署 | 支持 | 不支持（需要同时复制 dist） |
| 启动速度 | 快（内存读取） | 依赖磁盘 IO |

## API 说明

### public.Handler()

返回 `gin.HandlerFunc`，用于服务静态文件并支持 SPA 路由回退。

```go
import "github.com/anoixa/image-bed/public"

// 在 router 中使用
router.Use(public.Handler())

// 或者作为 404 fallback
router.NoRoute(public.Handler())
```

**路由优先级**：
以下路径会被自动跳过，交由后续路由处理：
- `/api/*` - API 接口
- `/images/*` - 图片访问
- `/thumbnails/*` - 缩略图
- `/health` - 健康检查
- `/version` - 版本信息
- `/metrics` - 监控指标
- `/swagger/*` - API 文档

其他所有路径都会尝试从 `dist/` 目录提供文件，找不到时回退到 `index.html`（SPA 支持）。

### 其他工具函数

```go
// 读取嵌入的文件内容
data, err := public.ReadFile("index.html")

// 检查文件是否存在
exists := public.Exists("assets/logo.png")

// 列出所有嵌入的文件
files, err := public.ListFiles()

// 获取文件系统（标准库兼容）
fs := public.FileSystem()

// 打印所有文件（调试）
public.PrintFiles()
```

## 纯 API 模式（无前端）

如果只想将本项目作为纯 API 服务器使用（例如只使用 API 上传图片，或使用自己的独立前端）：

### 方法 1：通过环境变量/配置文件禁用

```bash
# .env 文件
SERVE_FRONTEND=false
```

```go
// 代码中检查
if cfg.ServeFrontend {
    router.Use(public.Handler())
}
```

### 方法 2：构建时不包含 public 包

使用构建标签排除前端（可选方案，需要额外配置）：
```bash
go build -tags=nofrontend -o image-bed .
```

**优势对比：**

| 方案 | 构建产物大小 | 灵活性 | 适用场景 |
|------|-------------|--------|----------|
| `SERVE_FRONTEND=false` | 较大（包含前端代码但不使用） | 高（随时切换） | 需要灵活切换的场景 |
| 不复制 dist 目录 + dev 模式 | 较小（不包含前端） | 中 | 开发纯 API 功能 |
| 条件编译 | 最小（完全移除前端代码） | 低（需要重新编译切换） | 极致精简的 API 服务器 |

## 注意事项

1. **`.gitignore` 建议**：将 `public/dist/` 添加到 `.gitignore`，避免将构建产物提交到版本控制
2. **CI/CD**：在构建流程中加入前端构建步骤，然后将产物复制到此目录
3. **缓存**：生产模式下浏览器缓存由 `http.FileServer` 自动处理，根据文件修改时间计算 ETag
4. **纯 API 模式**：设置 `SERVE_FRONTEND=false` 可以完全禁用前端服务，此时根路径 `/` 会返回 404
