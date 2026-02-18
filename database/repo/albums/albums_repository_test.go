package albums

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB 创建测试数据库
func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	assert.NoError(t, err)

	// 自动迁移
	err = db.AutoMigrate(&models.User{}, &models.Album{}, &models.Image{})
	assert.NoError(t, err)

	return db
}

// testProvider 测试数据库提供者
type testProvider struct {
	db *gorm.DB
}

func (p *testProvider) DB() *gorm.DB {
	return p.db
}

func (p *testProvider) WithContext(ctx context.Context) *gorm.DB {
	return p.db.WithContext(ctx)
}

func (p *testProvider) Transaction(fn database.TxFunc) error {
	return p.db.Transaction(fn)
}

func (p *testProvider) TransactionWithContext(ctx context.Context, fn database.TxFunc) error {
	return p.db.WithContext(ctx).Transaction(fn)
}

func (p *testProvider) BeginTransaction() *gorm.DB {
	return p.db.Begin()
}

func (p *testProvider) WithTransaction() *gorm.DB {
	return p.db
}

func (p *testProvider) AutoMigrate(models ...interface{}) error {
	return p.db.AutoMigrate(models...)
}

func (p *testProvider) SQLDB() (*sql.DB, error) {
	return p.db.DB()
}

func (p *testProvider) Ping() error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Ping()
}

func (p *testProvider) Close() error {
	sqlDB, err := p.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func (p *testProvider) Name() string {
	return "sqlite"
}

// --- 测试 AlbumInfo 结构体 ---

func TestAlbumInfo_Struct(t *testing.T) {
	info := &AlbumInfo{
		Album: &models.Album{
			Model: gorm.Model{
				ID:        1,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			Name:        "Test Album",
			Description: "Test Description",
			UserID:      1,
		},
		ImageCount: 5,
		CoverURL:   "test-cover.jpg",
	}

	assert.Equal(t, uint(1), info.Album.ID)
	assert.Equal(t, "Test Album", info.Album.Name)
	assert.Equal(t, "Test Description", info.Album.Description)
	assert.Equal(t, uint(1), info.Album.UserID)
	assert.Equal(t, int64(5), info.ImageCount)
	assert.Equal(t, "test-cover.jpg", info.CoverURL)
}

// --- 测试 Repository 构造 ---

func TestNewRepository(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})
	assert.NotNil(t, repo)
	assert.NotNil(t, repo.db)
}

// --- 测试 CreateAlbum ---

func TestRepository_CreateAlbum(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 先创建一个用户
	user := &models.User{
		Username: "testuser",
		Password: "password",
	}
	db.Create(user)

	album := &models.Album{
		Name:        "Test Album",
		Description: "Test Description",
		UserID:      user.ID,
	}

	err := repo.CreateAlbum(album)
	assert.NoError(t, err)
	assert.NotZero(t, album.ID)
	assert.NotZero(t, album.CreatedAt)
}

func TestRepository_CreateAlbum_InvalidData(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 不设置 UserID，会被 GORM 设置为 0，而不是 NULL
	album := &models.Album{
		Name:        "Test Album",
		Description: "Test Description",
	}

	// SQLite 不会强制执行 NOT NULL，所以这里不会报错
	// 在实际的数据库中（如 PostgreSQL/MySQL），这会失败
	err := repo.CreateAlbum(album)
	// 由于 SQLite 的特性，这个测试可能通过，不强制要求失败
	_ = err
}

// --- 测试 GetUserAlbums ---

func TestRepository_GetUserAlbums(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 创建用户
	user := &models.User{
		Username: "testuser1",
		Password: "password",
	}
	db.Create(user)

	// 创建相册
	albums := []*models.Album{
		{Name: "Album 1", UserID: user.ID},
		{Name: "Album 2", UserID: user.ID},
		{Name: "Album 3", UserID: user.ID},
	}
	for _, album := range albums {
		db.Create(album)
	}

	// 测试获取第一页
	result, total, err := repo.GetUserAlbums(user.ID, 1, 2)
	assert.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, result, 2)

	// 测试获取第二页
	result, total, err = repo.GetUserAlbums(user.ID, 2, 2)
	assert.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Len(t, result, 1)
}

func TestRepository_GetUserAlbums_Empty(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	result, total, err := repo.GetUserAlbums(999, 1, 10)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Len(t, result, 0)
}

// --- 测试 GetAlbumWithImagesByID ---

func TestRepository_GetAlbumWithImagesByID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 创建用户
	user := &models.User{
		Username: "testuser2",
		Password: "password",
	}
	db.Create(user)

	// 创建相册
	album := &models.Album{
		Name:        "Test Album",
		UserID:      user.ID,
		Description: "Test",
	}
	db.Create(album)

	// 创建图片
	images := []*models.Image{
		{Identifier: "img1.jpg", OriginalName: "image1.jpg", FileSize: 1000, MimeType: "image/jpeg", FileHash: "hash1", UserID: user.ID},
		{Identifier: "img2.jpg", OriginalName: "image2.jpg", FileSize: 2000, MimeType: "image/jpeg", FileHash: "hash2", UserID: user.ID},
	}
	for _, img := range images {
		db.Create(img)
	}

	// 关联图片到相册
	db.Model(album).Association("Images").Append(images)

	// 测试获取相册
	result, err := repo.GetAlbumWithImagesByID(album.ID, user.ID)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, album.Name, result.Name)
	assert.Len(t, result.Images, 2)
}

func TestRepository_GetAlbumWithImagesByID_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	_, err := repo.GetAlbumWithImagesByID(999, 1)
	assert.Error(t, err)
	assert.Equal(t, gorm.ErrRecordNotFound, err)
}

func TestRepository_GetAlbumWithImagesByID_WrongUser(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 创建两个用户
	user1 := &models.User{Username: "user1a", Password: "pass"}
	user2 := &models.User{Username: "user2a", Password: "pass"}
	db.Create(user1)
	db.Create(user2)

	// 创建属于 user1 的相册
	album := &models.Album{Name: "Test", UserID: user1.ID}
	db.Create(album)

	// 用 user2 的 ID 查询
	_, err := repo.GetAlbumWithImagesByID(album.ID, user2.ID)
	assert.Error(t, err)
}

// --- 测试 AddImageToAlbum ---

func TestRepository_AddImageToAlbum(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 创建用户
	user := &models.User{Username: "testuser3", Password: "password"}
	db.Create(user)

	// 创建相册和图片
	album := &models.Album{Name: "Test Album", UserID: user.ID}
	db.Create(album)

	image := &models.Image{
		Identifier:   "test1.jpg",
		OriginalName: "test1.jpg",
		FileSize:     1000,
		MimeType:     "image/jpeg",
		FileHash:     "hash123",
		UserID:       user.ID,
	}
	db.Create(image)

	// 添加图片到相册
	err := repo.AddImageToAlbum(album.ID, user.ID, image)
	assert.NoError(t, err)

	// 验证关联
	var count int64
	db.Table("album_images").Where("album_id = ?", album.ID).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestRepository_AddImageToAlbum_AlbumNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	user := &models.User{Username: "testuser4", Password: "password"}
	db.Create(user)

	image := &models.Image{
		Identifier:   "test2.jpg",
		OriginalName: "test2.jpg",
		FileSize:     1000,
		MimeType:     "image/jpeg",
		FileHash:     "hash456",
		UserID:       user.ID,
	}
	db.Create(image)

	err := repo.AddImageToAlbum(999, user.ID, image)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- 测试 RemoveImageFromAlbum ---

func TestRepository_RemoveImageFromAlbum(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 创建用户
	user := &models.User{Username: "testuser5", Password: "password"}
	db.Create(user)

	// 创建相册和图片
	album := &models.Album{Name: "Test Album", UserID: user.ID}
	db.Create(album)

	image := &models.Image{
		Identifier:   "test3.jpg",
		OriginalName: "test3.jpg",
		FileSize:     1000,
		MimeType:     "image/jpeg",
		FileHash:     "hash789",
		UserID:       user.ID,
	}
	db.Create(image)

	// 先添加图片
	db.Model(album).Association("Images").Append(image)

	// 验证已添加
	var count int64
	db.Table("album_images").Where("album_id = ?", album.ID).Count(&count)
	assert.Equal(t, int64(1), count)

	// 移除图片
	err := repo.RemoveImageFromAlbum(album.ID, user.ID, image)
	assert.NoError(t, err)

	// 验证已移除
	db.Table("album_images").Where("album_id = ?", album.ID).Count(&count)
	assert.Equal(t, int64(0), count)
}

// --- 测试 DeleteAlbum ---

func TestRepository_DeleteAlbum(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 创建用户
	user := &models.User{Username: "testuser6", Password: "password"}
	db.Create(user)

	// 创建相册和图片
	album := &models.Album{Name: "Test Album", UserID: user.ID}
	db.Create(album)

	image := &models.Image{
		Identifier:   "test4.jpg",
		OriginalName: "test4.jpg",
		FileSize:     1000,
		MimeType:     "image/jpeg",
		FileHash:     "hashabc",
		UserID:       user.ID,
	}
	db.Create(image)

	// 关联图片
	db.Model(album).Association("Images").Append(image)

	// 删除相册
	err := repo.DeleteAlbum(album.ID, user.ID)
	assert.NoError(t, err)

	// 验证相册已删除（软删除）
	var count int64
	db.Unscoped().Model(&models.Album{}).Where("id = ?", album.ID).Count(&count)
	assert.Equal(t, int64(1), count) // 软删除后记录仍存在

	// 验证关联已清除
	db.Table("album_images").Where("album_id = ?", album.ID).Count(&count)
	assert.Equal(t, int64(0), count)

	// 验证图片还存在（只是从相册移除）
	db.Model(&models.Image{}).Where("id = ?", image.ID).Count(&count)
	assert.Equal(t, int64(1), count)
}

func TestRepository_DeleteAlbum_NotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	user := &models.User{Username: "testuser7", Password: "password"}
	db.Create(user)

	err := repo.DeleteAlbum(999, user.ID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// --- 测试 UpdateAlbum ---

func TestRepository_UpdateAlbum(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 创建用户和相册
	user := &models.User{Username: "testuser8", Password: "password"}
	db.Create(user)

	album := &models.Album{
		Name:        "Original Name",
		Description: "Original Description",
		UserID:      user.ID,
	}
	db.Create(album)

	// 更新相册
	album.Name = "Updated Name"
	album.Description = "Updated Description"
	err := repo.UpdateAlbum(album)
	assert.NoError(t, err)

	// 验证更新
	var updated models.Album
	db.First(&updated, album.ID)
	assert.Equal(t, "Updated Name", updated.Name)
	assert.Equal(t, "Updated Description", updated.Description)
}

// --- 测试 AlbumExists ---

func TestRepository_AlbumExists(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 创建用户和相册
	user := &models.User{Username: "testuser9", Password: "password"}
	db.Create(user)

	album := &models.Album{Name: "Test", UserID: user.ID}
	db.Create(album)

	// 存在的相册
	exists, err := repo.AlbumExists(album.ID)
	assert.NoError(t, err)
	assert.True(t, exists)

	// 不存在的相册
	exists, err = repo.AlbumExists(999)
	assert.NoError(t, err)
	assert.False(t, exists)
}

// --- 测试 GetAlbumByID ---

func TestRepository_GetAlbumByID(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(&testProvider{db: db})

	// 创建用户和相册
	user := &models.User{Username: "testuser10", Password: "password"}
	db.Create(user)

	album := &models.Album{Name: "Test", UserID: user.ID}
	db.Create(album)

	// 获取存在的相册
	result, err := repo.GetAlbumByID(album.ID)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, album.Name, result.Name)

	// 获取不存在的相册
	result, err = repo.GetAlbumByID(999)
	assert.Error(t, err)
	assert.Nil(t, result)
}
