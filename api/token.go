package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	jwtSecret           []byte
	jwtExpiresIn        time.Duration
	jwtRefreshExpiresIn time.Duration
)

// Init Initialize JWT configuration
func Init(secret, expiresIn, refreshExpiresIn string) error {
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

// GenerateRandomToken Generate random token
func GenerateRandomToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
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
	refreshToken, err = GenerateRandomToken(64)

	refreshTokenExpiry = time.Now().Add(jwtRefreshExpiresIn)
	return
}

// Parse Parse and validate JWT token
func Parse(tokenString string) (jwt.MapClaims, error) {
	if len(jwtSecret) == 0 {
		return nil, errors.New("JWT secret is not initialized")
	}

	if len(tokenString) > 7 && tokenString[:7] == "Bearer " {
		tokenString = tokenString[7:]
	}

	// 解析令牌
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

// ValidateAccessToken Verify access token
func ValidateAccessToken(tokenString string) (jwt.MapClaims, error) {
	claims, err := Parse(tokenString)
	if err != nil {
		return nil, err
	}

	// 验证令牌类型
	if tokenType, ok := claims["type"].(string); !ok || tokenType != "access" {
		return nil, errors.New("invalid token type")
	}

	return claims, nil
}
