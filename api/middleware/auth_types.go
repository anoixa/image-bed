package middleware

// 认证相关常量定义
const (
	// AuthSchemeBearer Bearer 认证方案
	AuthSchemeBearer = "Bearer"
	// AuthSchemeAPIKey API Key 认证方案
	AuthSchemeAPIKey = "ApiKey"
)

// 认证类型常量
const (
	// AuthTypeJWT JWT 认证类型
	AuthTypeJWT = "jwt"
	// AuthTypeStaticToken 静态令牌认证类型
	AuthTypeStaticToken = "static_token"
)

// 角色常量
const (
	// RoleUser 普通用户角色
	RoleUser = "user"
	// RoleAdmin 管理员角色
	RoleAdmin = "admin"
)

// 预定义的认证类型组合
var (
	// AllowJWTOnly 仅允许 JWT 认证
	AllowJWTOnly = []string{AuthTypeJWT}
	// AllowAllAuth 允许所有认证方式 (JWT + Static Token)
	AllowAllAuth = []string{AuthTypeJWT, AuthTypeStaticToken}
)

// Context 键常量
const (
	// ContextUserIDKey 用户 ID 上下文键
	ContextUserIDKey = "user_id"
	// ContextUsernameKey 用户名上下文键
	ContextUsernameKey = "username"
	// ContextRoleKey 角色上下文键
	ContextRoleKey = "role"
	// AuthTypeKey 认证类型上下文键
	AuthTypeKey = "auth_type"
)
