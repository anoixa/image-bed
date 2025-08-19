package images

import (
	"image-bed/api/common"
	"image-bed/database/images"
	"image-bed/database/models"
	"image-bed/storage"
	"image-bed/utils/validator"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// UploadImageHandler Image upload interface
func UploadImageHandler(context *gin.Context) {
	header, err := context.FormFile("file")
	if err != nil {
		common.RespondError(context, http.StatusBadRequest, "Empty files are not allowed to be uploaded.")
		return
	}

	// open file
	file, err := header.Open()
	if err != nil {
		common.RespondError(context, http.StatusInternalServerError, "Failed to open file")
		return
	}
	defer file.Close()

	// Verify image type
	isValidImage, err := validator.IsImage(file)
	if err != nil {
		log.Printf("Error during file validation: %v", err)
		common.RespondError(context, http.StatusInternalServerError, "Failed to validate file")
		return
	}
	if !isValidImage {
		common.RespondError(context, http.StatusBadRequest, "Unsupported image type or the file is corrupted")
		return
	}

	storagePath, err := storage.AppStorage.Save(file, header)
	if err != nil {
		log.Printf("Failed to save file: %v", err)
		common.RespondError(context, http.StatusInternalServerError, "Failed to save file")
		return
	}

	image := models.Image{
		FileName:    header.Filename,
		FileSize:    header.Size,
		MimeType:    header.Header.Get("Content-Type"),
		StoragePath: storagePath,
		UploadTime:  time.Now(),
	}

	err = images.SaveImage(&image)
	if err != nil {
		log.Printf("Failed to create image database record: %v", err)
		common.RespondError(context, http.StatusInternalServerError, "Failed to save image metadata")
		return
	}

	common.RespondSuccessMessage(context, "Image uploaded successfully", image.FileName)
}
