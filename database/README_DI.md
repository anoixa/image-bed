# Database DI 架构文档

本文档描述数据库层的依赖注入（DI）架构设计，参考了 storage 和 cache 层的重构模式。

## 架构概览

```
┌─────────────────────────────────────────────────────────────────┐
│                        DI Container                             │
│                    (internal/di/container.go)                   │
├─────────────────────────────────────────────────────────────────┤
│  Database Factory                                               │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  Provider Interface                                       │ │
│  │  ┌─────────────┐  ┌─────────────┐                        │ │
│  │  │  GormProvider│  │ (Mock)      │ ... 其他实现           │ │
│  │  │  (SQLite/    │  │  Provider   │                        │ │
│  │  │   PostgreSQL)│  │             │                        │ │
│  │  └─────────────┘  └─────────────┘                        │ │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        Repository Layer                         │
├─────────────────────────────────────────────────────────────────┤
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐             │
│  │  Images     │  │  Albums     │  │  Keys       │             │
│  │  Repository │  │  Repository │  │  Repository │             │
│  └─────────────┘  └─────────────┘  └─────────────┘             │
│  ┌─────────────┐  ┌─────────────┐                              │
│  │  Accounts   │  │  Devices    │                              │
│  │  Repository │  │  Repository │                              │
│  └─────────────┘  └─────────────┘                              │
└─────────────────────────────────────────────────────────────────┘
```

## 核心组件

### 1. Provider 接口 ([`database/provider.go`](database/provider.go:1))

定义数据库访问的基本操作：

```go
type Provider interface {
    DB() *gorm.DB
    WithContext(ctx context.Context) *gorm.DB
    Transaction(fn TxFunc) error
    TransactionWithContext(ctx context.Context, fn TxFunc) error
    BeginTransaction() *gorm.DB
    WithTransaction() *gorm.DB
    AutoMigrate(models ...interface{}) error
    SQLDB() (*sql.DB, error)
    Ping() error
    Close() error
    Name() string
}
```

### 2. GormProvider 实现 ([`database/gorm_provider.go`](database/gorm_provider.go:1))

基于 GORM 的数据库提供者，支持 SQLite 和 PostgreSQL：

```go
// 创建新的 GORM 提供者
provider, err := database.NewGormProvider(cfg)
```

### 3. Factory ([`database/factory.go`](database/factory.go:1))

数据库工厂负责创建和管理数据库提供者：

```go
// 创建工厂
factory, err := database.NewFactory(cfg)

// 获取提供者
provider := factory.GetProvider()

// 自动迁移
err = factory.AutoMigrate()
```

### 4. Repository 模式

每个领域都有对应的 Repository：

#### Images Repository ([`database/repo/images/repository.go`](database/repo/images/repository.go:1))

```go
repo := images.NewRepository(db)

// 保存图片
err := repo.SaveImage(image)

// 查询图片
img, err := repo.GetImageByHash(hash)
img, err := repo.GetImageByIdentifier(identifier)

// 删除图片
count, err := repo.DeleteImagesByIdentifiersAndUser(identifiers, userID)

// 带上下文
ctxRepo := repo.WithContext(ctx)
```

#### Albums Repository ([`database/repo/albums/repository.go`](database/repo/albums/repository.go:1))

```go
repo := albums.NewRepository(db)

// 创建相册
err := repo.CreateAlbum(album)

// 获取用户相册
albums, total, err := repo.GetUserAlbums(userID, page, pageSize)

// 添加/移除图片
err := repo.AddImageToAlbum(albumID, userID, image)
err := repo.RemoveImageFromAlbum(albumID, userID, image)
```

#### Accounts Repository ([`database/repo/accounts/repository.go`](database/repo/accounts/repository.go:1))

```go
repo := accounts.NewRepository(db)

// 创建默认管理员
repo.CreateDefaultAdminUser()

// 用户操作
user, err := repo.GetUserByUsername(username)
user, err := repo.GetUserByID(id)
err := repo.CreateUser(user)
```

#### Keys Repository ([`database/repo/keys/key.go`](database/repo/keys/key.go:1))

```go
repo := key.NewRepository(db)

// Token 操作
user, err := repo.GetUserByApiToken(token)
err := repo.CreateKey(apiToken)
err := repo.DisableApiToken(tokenID, userID)
```

## 在 DI Container 中使用

### 初始化

```go
// 创建容器
container := di.NewContainer(cfg)

// 初始化所有服务
err := container.Init()
```

### 获取数据库提供者

```go
// 获取提供者用于创建仓库
db := container.GetDatabaseProvider()

// 创建自定义仓库
repo := images.NewRepository(db)
```

### 向后兼容

对于现有的包级别函数，使用全局数据库提供者：

```go
// 在 DI Container 初始化时设置
key.SetDBProvider(container.GetDatabaseProvider())

// 然后在任何地方都可以使用包级别函数
user, err := key.GetUserByApiToken(token)
```

## 测试

Repository 模式使测试变得简单：

```go
// 创建 mock 提供者
mockDB := &MockProvider{}

// 创建仓库
repo := images.NewRepository(mockDB)

// 测试仓库方法
img, err := repo.GetImageByHash("test-hash")
```

### Mock Provider 示例

```go
type MockProvider struct {
    mockDB *gorm.DB // 使用 gorm 的测试 DB
}

func (m *MockProvider) DB() *gorm.DB {
    return m.mockDB
}

func (m *MockProvider) Transaction(fn database.TxFunc) error {
    return fn(m.mockDB)
}

// ... 其他方法实现
```

## 事务支持

Repository 支持事务操作：

```go
// 在仓库内部使用事务
func (r *Repository) SaveImage(image *models.Image) error {
    return r.db.Transaction(func(tx *gorm.DB) error {
        return tx.Create(&image).Error
    })
}

// 手动控制事务
tx := repo.db.BeginTransaction()
// ... 操作
repo.db.Transaction(func(tx *gorm.DB) error {
    // ... 多个操作
    return nil
})
```

## 最佳实践

1. **优先使用 Repository 模式**：不要直接调用 `db.DB().Where(...)`，而是使用 Repository 方法

2. **依赖注入**：将 Repository 作为参数传递给 handler：
   ```go
   type ImageHandler struct {
       repo *images.Repository
   }
   
   func NewImageHandler(repo *images.Repository) *ImageHandler {
       return &ImageHandler{repo: repo}
   }
   ```

3. **上下文传播**：对于需要取消或超时的操作，使用 `WithContext`：
   ```go
   ctxRepo := repo.WithContext(ctx)
   img, err := ctxRepo.GetImageByID(id)
   ```

4. **事务边界**：在 Repository 层封装事务，handler 层不需要关心事务

## 迁移指南

### 从旧代码迁移

**旧代码：**
```go
import "github.com/anoixa/image-bed/database/dbcore"

func handler() {
    db := dbcore.GetDBInstance()
    var image models.Image
    db.Where("id = ?", id).First(&image)
}
```

**新代码：**
```go
import "github.com/anoixa/image-bed/database/repo/images"

func handler(repo *images.Repository) {
    image, err := repo.GetImageByID(id)
}
```

### 包级别函数的向后兼容

对于不想立即迁移的代码，可以继续使用包级别函数，只需在应用启动时设置数据库提供者：

```go
// main.go
key.SetDBProvider(container.GetDatabaseProvider())

// 然后在 handler 中
user, err := key.GetUserByApiToken(token)
```
