package vdoc

import "context"

// MCPTokenAuthRepository 在同一事务内锁定令牌与用户，将领域判定交给应用层。
// visit 可更新令牌使用时间或过期状态，并返回必须原子写入的审计。
type MCPTokenAuthRepository interface {
	UseMCPToken(context.Context, string, func(*MCPToken, *User) *AuditLog) error
}
