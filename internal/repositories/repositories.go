package repositories

import (
	"github.com/anoixa/image-bed/database"
	"github.com/anoixa/image-bed/database/repo/accounts"
	"github.com/anoixa/image-bed/database/repo/albums"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/database/repo/keys"
)

// Repositories 集中管理所有数据库仓库
type Repositories struct {
	Accounts *accounts.Repository
	Devices  *accounts.DeviceRepository
	Images   *images.Repository
	Albums   *albums.Repository
	Keys     *keys.Repository
}

// NewRepositories 创建所有仓库实例
func NewRepositories(provider database.Provider) *Repositories {
	return &Repositories{
		Accounts: accounts.NewRepository(provider),
		Devices:  accounts.NewDeviceRepository(provider),
		Images:   images.NewRepository(provider),
		Albums:   albums.NewRepository(provider),
		Keys:     keys.NewRepository(provider),
	}
}
