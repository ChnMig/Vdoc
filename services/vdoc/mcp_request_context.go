package vdoc

import "context"

type mcpTokenContextKey struct{}

// WithMCPToken 保留认证时使用的令牌身份，供提交操作时重新校验。
func WithMCPToken(ctx context.Context, tokenID string) context.Context {
	return context.WithValue(ctx, mcpTokenContextKey{}, tokenID)
}

func mcpMutationTokenID(ctx context.Context) string {
	tokenID, _ := ctx.Value(mcpTokenContextKey{}).(string)
	return tokenID
}
