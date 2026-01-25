package api

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/anoixa/image-bed/cache"
	"github.com/anoixa/image-bed/database/models"
	"github.com/anoixa/image-bed/database/repo/keys"
	"github.com/anoixa/image-bed/utils"
	"golang.org/x/sync/singleflight"

	"github.com/golang-jwt/jwt/v5"
)

var staticTokenGroup singleflight.Group

var (
	jwtSecret           []byte
	jwtExpiresIn        time.Duration
	jwtRefreshExpiresIn time.Duration
)

// TokenInit Initialize JWT configuration
func TokenInit(secret, expiresIn, refreshExpiresIn string) error {
	jwtSecret = []byte(secret)

	duration, err := time.ParseDuration(expiresIn)
	if err != nil {
		return fmt.Errorf("invalid JWT expiration duration: %s", expiresIn)
	}
	jwtExpiresIn = duration

	refreshDuration, err := time.ParseDuration(refreshExpiresIn)
	if err != nil {
		return fmt.Errorf("invalid JWT refresh expiration duration: %s", refreshExpiresIn)
	}
	jwtRefreshExpiresIn = refreshDuration

	log.Printf("JWT Config loaded - Access: %v, Refresh: %v\n", jwtExpiresIn, jwtRefreshExpiresIn)

	return nil
}

// GenerateTokens Generate access token and refresh token
func GenerateTokens(username string, userID uint) (accessToken string, accessTokenExpiry time.Time, err error) {
	if len(jwtSecret) == 0 {
		err = errors.New("JWT secret is not initialized")
		return
	}

	// 生成access token (JWT)
	accessTokenExpiry = time.Now().Add(jwtExpiresIn)
	accessClaims := jwt.MapClaims{
		"username": username,
		"user_id":  userID,
		"type":     "access",
		"exp":      accessTokenExpiry.Unix(),
		"iat":      time.Now().Unix(),
	}

	accessToken, err = jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(jwtSecret)
	if err != nil {
		err = fmt.Errorf("failed to generate access token: %w", err)
		// 重置返回值
		accessToken = ""
		accessTokenExpiry = time.Time{}
		return
	}

	return
}

// GenerateRefreshToken Generate refresh token
func GenerateRefreshToken() (refreshToken string, refreshTokenExpiry time.Time, err error) {
	refreshToken, err = utils.GenerateRandomToken(64)

	refreshTokenExpiry = time.Now().Add(jwtRefreshExpiresIn)
	return
}

// GenerateStaticToken Generate a new static key
func GenerateStaticToken() (refreshToken string, err error) {
	refreshToken, err = utils.GenerateRandomToken(64)

	return
}

// Parse Parse and validate JWT token
func Parse(tokenString string) (jwt.MapClaims, error) {
	if len(jwtSecret) == 0 {
		return nil, errors.New("JWT secret is not initialized")
	}

	// 解析令牌
	// 注意：调用者（auth_middleware.go）已经处理了 Authorization 头中的 "Bearer " 前缀
	// 这里不再重复移除
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	// 验证令牌有效性
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}

// ValidateStaticToken Verify static token
func ValidateStaticToken(tokenString string) (*models.User, error) {
	var cachedUser models.User
	if err := cache.GetCachedStaticToken(tokenString, &cachedUser); err == nil {
		// 缓存命中
		return &cachedUser, nil
	} else if !cache.IsCacheMiss(err) {
		log.Printf("Cache error when fetching static token: %v", err)
	}

	val, err, _ := staticTokenGroup.Do(fmt.Sprintf("static_token_%s", tokenString), func() (interface{}, error) {
		// 缓存未命中
		user, err := key.GetUserByApiToken(tokenString)
		if err != nil {
			return nil, err
		}
		if user == nil {
			cacheKey := fmt.Sprintf("%s%s", cache.StaticTokenCachePrefix, tokenString)
			if cacheErr := cache.CacheEmptyValue(cacheKey); cacheErr != nil {
				log.Printf("Failed to cache empty static token: %v", cacheErr)
			}
			return nil, errors.New("invalid static token")
		}

		go func(user *models.User) {
			if cacheErr := cache.CacheStaticToken(tokenString, user); cacheErr != nil {
				log.Printf("Failed to cache static token: %v", cacheErr)
			}
		}(user)

		return user, nil
	})

	if err != nil {
		return nil, err
	}

	return val.(*models.User), nil
}
