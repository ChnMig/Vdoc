package vdoc

import (
	"context"
	"slices"
)

type mcpTokenContextKey struct{}
type mcpTokenContext struct {
	ID     string
	Scopes []int
}

// WithMCPToken 保留认证时使用的令牌身份，供提交操作时重新校验。
func WithMCPToken(ctx context.Context, tokenID string, scopes ...int) context.Context {
	return context.WithValue(ctx, mcpTokenContextKey{}, mcpTokenContext{ID: tokenID, Scopes: slices.Clone(scopes)})
}

func mcpMutationTokenID(ctx context.Context) string {
	token, _ := ctx.Value(mcpTokenContextKey{}).(mcpTokenContext)
	return token.ID
}

func mcpAuthenticatedScopes(ctx context.Context) []int {
	token, _ := ctx.Value(mcpTokenContextKey{}).(mcpTokenContext)
	return slices.Clone(token.Scopes)
}
