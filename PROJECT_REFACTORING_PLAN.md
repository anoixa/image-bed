# 项目重构计划

## ✅ 已完成的修复

### 1. 存储工厂未关闭问题
- **文件**: `internal/app/app.go`, `storage/factory.go`
- **修改**: 在 `Close()` 方法中添加 `storageFactory.Close()` 调用，补充 `Close()` 方法

### 2. GetThumbnailScanner 参数被忽略
- **文件**: `internal/app/app.go`
- **修改**: 删除此方法，调用方已在 `cmd/serve.go` 中直接使用 `image.NewThumbnailScanner`

### 3. 删除重复方法 GetDB
- **文件**: `database/factory.go`
- **修改**: 删除 `GetDB()`，保留 `DB()`，内联实现

### 4. 删除冗余密码长度检查
- **文件**: `database/repo/accounts/repository.go`
- **修改**: 删除不会执行的 `len(randomPassword) > 16` 检查

### 5. 修复错误处理不一致
- **文件**: `database/repo/accounts/repository.go`
- **修改**: 新增 `ErrUserNotFound`，`GetUserByUsername/GetUserByID` 返回明确错误

### 6. 仓库文件命名统一
- **文件**: `database/repo/keys/key.go` → `database/repo/keys/repository.go`
- **修改**: 统一命名为 `repository.go`

### 7. 修复测试期望
- **文件**: `api/handler/images/thumbnail_handler_test.go`
- **修改**: 更新缩略图后缀期望从 `.jpg` 到 `.webp`

---

## 待修复的问题

### 🔴 高优先级

#### 1. 包结构不一致

**storage/ 目录重构**:
```
storage/
├── provider.go          # 接口定义
├── factory.go           # 工厂
├── local/
│   ├── local.go         # 原 local.go
│   └── local_test.go
└── minio/
    └── minio.go         # 原 minio.go
```

**database/ 目录重构**:
```
database/
├── provider.go          # 接口定义
├── factory.go           # 工厂
├── gorm/
│   └── gorm_provider.go # 原 gorm_provider.go
└── ...
```

#### 2. 文件命名不一致
- `database/repo/keys/key.go` → `database/repo/keys/repository.go`

### 🟡 中优先级

#### 3. 职责重叠 - internal/repositories
**问题**: `internal/repositories` 只是简单聚合 `database/repo`，没有增加价值

**建议方案 A - 删除中间层**:
```go
// 直接在 Container 中使用 database/repo
 type Container struct {
     // ...
     AccountsRepo *accounts.Repository
     ImagesRepo   *images.Repository
     // ...
 }
```

**建议方案 B - 增强中间层**:
让 `internal/repositories` 提供事务管理、缓存等跨领域功能

#### 4. utils/ 职责混乱
**建议**:
```
utils/                    # 纯工具函数
├── log.go
├── mime.go
├── random.go
├── url.go
├── format/
├── pool/
└── validator/

internal/services/crypto/ # 从 utils/crypto 迁移
internal/worker/          # 从 utils/async 迁移
```

#### 5. API Handler 结构统一
**建议**: 统一按领域组织，每个领域一个 `handler.go`
```
api/handler/
├── admin.go       # 合并 config_handler.go + conversion_handler.go
├── albums.go      # 合并 albums/*_handler.go
├── images.go      # 合并 images/*_handler.go
└── keys.go        # 合并 key/*_handler.go
```

### 🟢 低优先级

#### 6. 配置管理合并
考虑将 `config/config.go` 和 `internal/services/config/` 合并或明确职责边界

---

## 依赖关系图

```
api/
├── handler/           → internal/services, internal/repositories
├── middleware/        → config, utils
└── core/              → config, internal/app

internal/
├── app/               → cache, database, storage, internal/services, internal/repositories
├── repositories/      → database/repo/*
└── services/          → database/repo/*, storage, cache

database/
├── repo/*             → database (models)
└── models/            → (无依赖)

storage/               → internal/services/crypto (❌ 反向依赖)
cache/                 → internal/services/crypto (❌ 反向依赖)
```

**问题**: storage 和 cache 依赖 internal/services/crypto，违反了分层架构原则

**建议**: 将加密服务移动到更底层的位置，如 `pkg/crypto` 或保持独立但不被底层依赖
