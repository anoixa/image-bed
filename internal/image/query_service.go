package image

import (
	"context"
	"fmt"
	"strings"

	"github.com/anoixa/image-bed/config"
	dbconfig "github.com/anoixa/image-bed/config/db"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
)

// QueryService 负责图片查询与元数据更新相关用例
type QueryService struct {
	repo          *images.Repository
	configManager *dbconfig.Manager
}

func NewQueryService(repo *images.Repository, configManager *dbconfig.Manager) *QueryService {
	return &QueryService{
		repo:          repo,
		configManager: configManager,
	}
}

// ListImages 获取图片列表
func (s *QueryService) ListImages(storageType string, identifier string, search string, albumID *uint, startTime, endTime int64, sort string, page int, limit int, userID int) (*ListImagesResult, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = config.DefaultPerPage
	}

	if limit > config.MaxPerPage {
		limit = config.MaxPerPage
	}

	storageConfigIDs, err := s.resolveStorageConfigIDs(context.Background(), storageType)
	if err != nil {
		return nil, err
	}
	if storageType != "" && len(storageConfigIDs) == 0 {
		return &ListImagesResult{
			Images:     []*models.Image{},
			Total:      0,
			Page:       page,
			Limit:      limit,
			TotalPages: 0,
		}, nil
	}

	list, total, err := s.repo.GetImageList(storageConfigIDs, identifier, search, albumID, startTime, endTime, sort, page, limit, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get image list: %w", err)
	}

	totalPages := int(total) / limit
	if int(total)%limit > 0 {
		totalPages++
	}

	return &ListImagesResult{
		Images:     list,
		Total:      total,
		Page:       page,
		Limit:      limit,
		TotalPages: totalPages,
	}, nil
}

func (s *QueryService) resolveStorageConfigIDs(ctx context.Context, storageType string) ([]uint, error) {
	if storageType == "" {
		return nil, nil
	}
	if s.configManager == nil {
		return nil, fmt.Errorf("failed to resolve storage filter: config manager not initialized")
	}

	configs, err := s.configManager.GetStorageConfigs(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve storage filter: %w", err)
	}

	normalizedType := strings.TrimSpace(strings.ToLower(storageType))
	ids := make([]uint, 0, len(configs))
	for _, cfg := range configs {
		if strings.EqualFold(cfg.Type, normalizedType) {
			ids = append(ids, cfg.ID)
		}
	}

	return ids, nil
}

// GetImageByIdentifier 获取图片
func (s *QueryService) GetImageByIdentifier(identifier string) (*models.Image, error) {
	return s.repo.GetImageByIdentifier(identifier)
}

// UpdateImageByIdentifier 更新图片
func (s *QueryService) UpdateImageByIdentifier(identifier string, updates map[string]any) (*models.Image, error) {
	return s.repo.UpdateImageByIdentifier(identifier, updates)
}
