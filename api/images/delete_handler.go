package images

import (
	"net/http"

	"github.com/anoixa/image-bed/api/common"
	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/repo/images"
	"github.com/gin-gonic/gin"
)

type DeleteRequestBody struct {
	ImageID []string `json:"identifier" binding:"required"`
}

// DeleteImagesHandler 删除图片
func DeleteImagesHandler(context *gin.Context) {
	var requestBody DeleteRequestBody
	if err := context.ShouldBindJSON(&requestBody); err != nil {
		common.RespondError(context, http.StatusBadRequest, err.Error())
		return
	}

	err := images.DeleteImagesByIdentifiers(requestBody.ImageID)
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to delete images")
		return
	}

	// 清除缓存(元数据与图片本体)
	for _, imageID := range requestBody.ImageID {
		_ = cache.DeleteCachedImage(imageID)
		_ = cache.DeleteCachedImageData(imageID)
	}

	common.RespondSuccessMessage(context, "Delete successful", nil)
}
