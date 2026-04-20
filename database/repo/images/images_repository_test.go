package images

import (
	"fmt"
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB 创建测试数据库（每个测试独立）
func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	// 自动迁移表结构
	err = db.AutoMigrate(&models.Image{}, &models.Album{})
	require.NoError(t, err)

	return db
}

func TestRepository_SaveImage(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	image := &models.Image{
		Identifier:   "test-123",
		OriginalName: "test.jpg",
		FileHash:     "hash123",
		FileSize:     1024,
		MimeType:     "image/jpeg",
		StoragePath:  "uploads/test.jpg",
		UserID:       1,
		IsPublic:     true,
	}

	err := repo.SaveImage(image)
	require.NoError(t, err)
	assert.NotZero(t, image.ID)
}

func TestRepository_GetImageByIdentifier(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	// 先保存图片
	image := &models.Image{
		Identifier:   "get-test",
		OriginalName: "get.jpg",
		FileHash:     "gethash",
		FileSize:     2048,
		MimeType:     "image/png",
		StoragePath:  "uploads/get.jpg",
		UserID:       1,
	}
	err := repo.SaveImage(image)
	require.NoError(t, err)

	// 测试获取
	found, err := repo.GetImageByIdentifier("get-test")
	require.NoError(t, err)
	assert.Equal(t, "get-test", found.Identifier)
	assert.Equal(t, "get.jpg", found.OriginalName)

	// 测试获取不存在的
	_, err = repo.GetImageByIdentifier("not-exist")
	assert.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestRepository_GetImageByHash(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	image := &models.Image{
		Identifier:   "hash-test",
		OriginalName: "hash.jpg",
		FileHash:     "uniquehash123",
		FileSize:     1024,
		MimeType:     "image/jpeg",
		StoragePath:  "uploads/hash.jpg",
		UserID:       1,
	}
	err := repo.SaveImage(image)
	require.NoError(t, err)

	found, err := repo.GetImageByHash("uniquehash123")
	require.NoError(t, err)
	assert.Equal(t, "hash-test", found.Identifier)
}

func TestRepository_DeleteImage(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	image := &models.Image{
		Identifier:   "delete-test",
		OriginalName: "delete.jpg",
		FileHash:     "deletehash",
		FileSize:     1024,
		MimeType:     "image/jpeg",
		StoragePath:  "uploads/delete.jpg",
		UserID:       1,
	}
	err := repo.SaveImage(image)
	require.NoError(t, err)

	// 删除
	err = repo.DeleteImage(image)
	require.NoError(t, err)

	// 确认删除
	_, err = repo.GetImageByIdentifier("delete-test")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestRepository_CountImagesByStorageConfig(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	// 创建多张图片
	images := []*models.Image{
		{Identifier: "s1", OriginalName: "1.jpg", FileHash: "h1", StorageConfigID: 1, UserID: 1},
		{Identifier: "s2", OriginalName: "2.jpg", FileHash: "h2", StorageConfigID: 1, UserID: 1},
		{Identifier: "s3", OriginalName: "3.jpg", FileHash: "h3", StorageConfigID: 2, UserID: 1},
	}

	for _, img := range images {
		err := repo.SaveImage(img)
		require.NoError(t, err)
	}

	count, err := repo.CountImagesByStorageConfig(1)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	count, err = repo.CountImagesByStorageConfig(2)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	count, err = repo.CountImagesByStorageConfig(999)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestRepository_ImageExists(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	image := &models.Image{
		Identifier:   "exist-test",
		OriginalName: "exist.jpg",
		FileHash:     "existhash",
		FileSize:     1024,
		MimeType:     "image/jpeg",
		StoragePath:  "uploads/exist.jpg",
		UserID:       1,
	}
	err := repo.SaveImage(image)
	require.NoError(t, err)

	exists, err := repo.ImageExists("exist-test")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = repo.ImageExists("not-exist")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRepository_ListImagesByUser(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	// 创建用户 1 的图片
	for i := 1; i <= 5; i++ {
		img := &models.Image{
			Identifier:   fmt.Sprintf("user1-%d", i),
			OriginalName: "1.jpg",
			FileHash:     fmt.Sprintf("h1-%d", i),
			UserID:       1,
		}
		err := repo.SaveImage(img)
		require.NoError(t, err)
	}

	// 创建用户 2 的图片
	for i := 1; i <= 3; i++ {
		img := &models.Image{
			Identifier:   fmt.Sprintf("user2-%d", i),
			OriginalName: "2.jpg",
			FileHash:     fmt.Sprintf("h2-%d", i),
			UserID:       2,
		}
		err := repo.SaveImage(img)
		require.NoError(t, err)
	}

	// 测试分页
	images, total, err := repo.ListImagesByUser(1, 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, images, 2)
}

func TestRepository_GetImageListFiltersByStorageConfigIDs(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	images := []*models.Image{
		{Identifier: "local-1", OriginalName: "local.jpg", FileHash: "filter-h1", UserID: 1, StorageConfigID: 10},
		{Identifier: "s3-1", OriginalName: "s3.jpg", FileHash: "filter-h2", UserID: 1, StorageConfigID: 20},
		{Identifier: "other-user", OriginalName: "other.jpg", FileHash: "filter-h3", UserID: 2, StorageConfigID: 10},
	}

	for _, image := range images {
		require.NoError(t, repo.SaveImage(image))
	}

	result, total, err := repo.GetImageList([]uint{10}, "", "", nil, 0, 0, "desc", 1, 10, 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, result, 1)
	assert.Equal(t, "local-1", result[0].Identifier)
}

func TestRepository_DeleteImageByIdentifierAndUser(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	image := &models.Image{
		Identifier:   "del-id-test",
		OriginalName: "del.jpg",
		FileHash:     "delhash",
		StoragePath:  "uploads/del.jpg",
		UserID:       1,
	}
	err := repo.SaveImage(image)
	require.NoError(t, err)

	// 错误的用户删除
	err = repo.DeleteImageByIdentifierAndUser("del-id-test", 2)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	// 正确的用户删除
	err = repo.DeleteImageByIdentifierAndUser("del-id-test", 1)
	require.NoError(t, err)

	// 确认删除
	_, err = repo.GetImageByIdentifier("del-id-test")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestRepository_GetSoftDeletedImageByHash(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	image := &models.Image{
		Identifier:   "soft-del",
		OriginalName: "soft.jpg",
		FileHash:     "softhash",
		StoragePath:  "uploads/soft.jpg",
		UserID:       1,
	}
	err := repo.SaveImage(image)
	require.NoError(t, err)

	// 软删除
	err = repo.DeleteImage(image)
	require.NoError(t, err)

	// GetImageByHash uses Unscoped, so it finds soft-deleted records too
	found, err := repo.GetImageByHash("softhash")
	require.NoError(t, err)
	assert.Equal(t, "soft-del", found.Identifier)
	assert.NotNil(t, found.DeletedAt)

	// GetSoftDeletedImageByHash also works
	found2, err := repo.GetSoftDeletedImageByHash("softhash")
	require.NoError(t, err)
	assert.Equal(t, "soft-del", found2.Identifier)
	assert.NotNil(t, found2.DeletedAt)
}

func TestRepository_UpdateImageByIdentifier(t *testing.T) {
	db := setupTestDB(t)
	repo := NewRepository(db)

	image := &models.Image{
		Identifier:   "update-test",
		OriginalName: "old.jpg",
		FileHash:     "oldhash",
		StoragePath:  "uploads/old.jpg",
		UserID:       1,
		IsPublic:     false,
	}
	err := repo.SaveImage(image)
	require.NoError(t, err)

	// 更新
	updates := map[string]any{
		"original_name": "new.jpg",
		"is_public":     true,
	}
	updated, err := repo.UpdateImageByIdentifier("update-test", updates)
	require.NoError(t, err)
	assert.Equal(t, "new.jpg", updated.OriginalName)
	assert.True(t, updated.IsPublic)

	// 更新不存在的
	_, err = repo.UpdateImageByIdentifier("not-exist", updates)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
