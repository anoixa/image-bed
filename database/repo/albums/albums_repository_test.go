package albums

import (
	"testing"

	"github.com/anoixa/image-bed/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupAlbumsRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=private"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}, &models.Album{}, &models.Image{}))
	return db
}

func TestAddImagesToAlbumIsIdempotent(t *testing.T) {
	db := setupAlbumsRepositoryTestDB(t)
	repo := NewRepository(db)

	user := &models.User{Username: "owner", Password: "hash", Role: models.RoleUser}
	require.NoError(t, db.Create(user).Error)

	album := &models.Album{UserID: user.ID, Name: "album"}
	require.NoError(t, db.Create(album).Error)

	imageOne := &models.Image{
		Identifier:      "img-1",
		OriginalName:    "one.jpg",
		FileHash:        "hash-1",
		FileSize:        123,
		MimeType:        "image/jpeg",
		StoragePath:     "original/one.jpg",
		StorageConfigID: 1,
		UserID:          user.ID,
	}
	imageTwo := &models.Image{
		Identifier:      "img-2",
		OriginalName:    "two.jpg",
		FileHash:        "hash-2",
		FileSize:        456,
		MimeType:        "image/jpeg",
		StoragePath:     "original/two.jpg",
		StorageConfigID: 1,
		UserID:          user.ID,
	}
	require.NoError(t, db.Create(imageOne).Error)
	require.NoError(t, db.Create(imageTwo).Error)

	inserted, err := repo.AddImagesToAlbum(album.ID, user.ID, []uint{imageOne.ID, imageOne.ID, imageTwo.ID})
	require.NoError(t, err)
	assert.Equal(t, int64(2), inserted)

	inserted, err = repo.AddImagesToAlbum(album.ID, user.ID, []uint{imageOne.ID, imageTwo.ID})
	require.NoError(t, err)
	assert.Equal(t, int64(0), inserted)

	var count int64
	require.NoError(t, db.Table("album_images").Where("album_id = ?", album.ID).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}
