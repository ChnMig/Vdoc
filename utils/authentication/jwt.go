package authentication

import (
	"fmt"
	"strings"
	"time"

	"vdoc/config"
	"vdoc/utils/id"

	"github.com/golang-jwt/jwt/v5"
)

var (
	defaultIssuer   = "vdoc"
	defaultSubject  = "token"
	defaultAudience = "client"
)

// MapClaims 保存本项目 JWT 需要的最小自定义数据。
type MapClaims struct {
	Data map[string]any `json:"data"`
	jwt.RegisteredClaims
}

// PrepareRegisteredClaims 填充默认的 RegisteredClaims 字段
func PrepareRegisteredClaims(rc *jwt.RegisteredClaims) {
	if rc == nil {
		return
	}
	now := time.Now()
	if rc.Issuer == "" {
		rc.Issuer = defaultIssuer
	}
	if rc.Subject == "" {
		rc.Subject = defaultSubject
	}
	if len(rc.Audience) == 0 {
		rc.Audience = jwt.ClaimStrings{defaultAudience}
	}
	if rc.ID == "" {
		rc.ID = id.GenerateID()
	}
	if rc.IssuedAt == nil {
		rc.IssuedAt = jwt.NewNumericDate(now)
	}
	if rc.NotBefore == nil {
		rc.NotBefore = jwt.NewNumericDate(now)
	}
	if rc.ExpiresAt == nil && config.JWTExpiration > 0 {
		rc.ExpiresAt = jwt.NewNumericDate(now.Add(config.JWTExpiration))
	}
}

// SignHS256 使用 HS256 对 claims 进行签名
func SignHS256(claims jwt.Claims) (string, error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(config.JWTKey))
}

// ParseHS256 使用 HS256 验证并解析 token，结果写入传入的 claims
func ParseHS256(tokenString string, claims jwt.Claims) (*jwt.Token, error) {
	return jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(config.JWTKey), nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(defaultIssuer),
		jwt.WithSubject(defaultSubject),
		jwt.WithAudience(defaultAudience),
	)
}

// JWTIssue 签发仅包含必要用户身份的 JWT Token。
func JWTIssue(data map[string]any) (string, error) {
	userID, ok := data["user_id"].(string)
	if !ok || strings.TrimSpace(userID) == "" {
		return "", fmt.Errorf("user_id is required")
	}
	claims := MapClaims{Data: map[string]any{"user_id": userID}}
	PrepareRegisteredClaims(&claims.RegisteredClaims)
	return SignHS256(&claims)
}

// JWTDecrypt 解析 JWT Token，返回 map 数据
func JWTDecrypt(tokenString string) (map[string]any, error) {
	claims := &MapClaims{}
	token, err := ParseHS256(tokenString, claims)
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims.Data, nil
}
