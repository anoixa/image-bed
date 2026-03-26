package image

import (
	"context"
	"errors"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/anoixa/image-bed/utils"
)

// ReadService 负责图片读取相关用例
type ReadService struct {
	repo           *images.Repository
	variantService *VariantService
	converter      *Converter
	cacheHelper    *cache.Helper
	baseURL        string
	submitTask     func(func())
}

func NewReadService(
	repo *images.Repository,
	variantService *VariantService,
	converter *Converter,
	cacheHelper *cache.Helper,
	baseURL string,
	submitTask func(func()),
) *ReadService {
	return &ReadService{
		repo:           repo,
		variantService: variantService,
		converter:      converter,
		cacheHelper:    cacheHelper,
		baseURL:        baseURL,
		submitTask:     submitTask,
	}
}

// GetImageMetadata 获取图片元数据
func (s *ReadService) GetImageMetadata(ctx context.Context, identifier string) (*models.Image, error) {
	var image models.Image

	if s.cacheHelper != nil {
		if err := s.cacheHelper.GetCachedImage(ctx, identifier, &image); err == nil {
			return &image, nil
		}
	}

	resultChan := imageGroup.DoChan(identifier, func() (any, error) {
		imagePtr, err := s.repo.GetImageByIdentifier(identifier)
		if err != nil {
			if isTransientError(err) {
				return nil, ErrTemporaryFailure
			}
			return nil, err
		}

		go func(img *models.Image) {
			defer func() {
				if r := recover(); r != nil {
					utils.LogIfDevf("Panic in async cache goroutine for '%s': %v", img.Identifier, r)
				}
			}()
			if s.cacheHelper == nil {
				return
			}

			cacheCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if cacheErr := s.cacheHelper.CacheImage(cacheCtx, img); cacheErr != nil {
				utils.LogIfDevf("Failed to cache image metadata for '%s': %v", img.Identifier, cacheErr)
			}
		}(imagePtr)

		return imagePtr, nil
	})

	select {
	case result := <-resultChan:
		if result.Err != nil {
			if errors.Is(result.Err, ErrTemporaryFailure) {
				imageGroup.Forget(identifier)
			}
			return nil, result.Err
		}
		return result.Val.(*models.Image), nil
	case <-time.After(metaFetchTimeout):
		imageGroup.Forget(identifier)
		return nil, ErrTemporaryFailure
	}
}

// CheckImagePermission 检查图片访问权限
func (s *ReadService) CheckImagePermission(image *models.Image, userID uint) bool {
	if image.IsPublic {
		return true
	}
	return userID != 0 && userID == image.UserID
}

// GetImageWithVariant 获取图片
func (s *ReadService) GetImageWithVariant(ctx context.Context, identifier string, acceptHeader string, userID uint) (*ImageResultDTO, error) {
	image, err := s.GetImageMetadata(ctx, identifier)
	if err != nil {
		return nil, err
	}

	if !s.CheckImagePermission(image, userID) {
		return nil, ErrForbidden
	}

	return s.buildImageResult(ctx, image, acceptHeader), nil
}

// GetRandomImageWithVariant 获取随机图片
func (s *ReadService) GetRandomImageWithVariant(ctx context.Context, filter *images.RandomImageFilter, acceptHeader string) (*ImageResultDTO, error) {
	image, err := s.repo.GetRandomPublicImage(filter)
	if err != nil {
		return nil, err
	}

	return s.buildImageResult(ctx, image, acceptHeader), nil
}

func (s *ReadService) buildImageResult(ctx context.Context, image *models.Image, acceptHeader string) *ImageResultDTO {
	if s.variantService == nil {
		return &ImageResultDTO{
			Image:      image,
			IsOriginal: true,
			URL:        utils.BuildImageURL(s.baseURL, image.Identifier),
			MIMEType:   image.MimeType,
		}
	}

	variantResult, err := s.variantService.SelectBestVariant(ctx, image, acceptHeader)
	if err != nil {
		return &ImageResultDTO{
			Image:      image,
			IsOriginal: true,
			URL:        utils.BuildImageURL(s.baseURL, image.Identifier),
			MIMEType:   image.MimeType,
		}
	}

	if variantResult.ShouldTriggerConversion && s.converter != nil && s.submitTask != nil {
		s.submitTask(func() {
			s.converter.TriggerConversion(image)
		})
	}

	result := &ImageResultDTO{
		Image:      image,
		IsOriginal: variantResult.IsOriginal,
		MIMEType:   variantResult.MIMEType,
	}

	if variantResult.IsOriginal {
		result.URL = utils.BuildImageURL(s.baseURL, image.Identifier)
		result.MIMEType = image.MimeType
		return result
	}

	result.Variant = variantResult.Variant
	if variantResult.Variant != nil {
		result.URL = utils.BuildImageURL(s.baseURL, variantResult.Variant.Identifier)
		return result
	}

	result.IsOriginal = true
	result.URL = utils.BuildImageURL(s.baseURL, image.Identifier)
	result.MIMEType = image.MimeType
	return result
}
