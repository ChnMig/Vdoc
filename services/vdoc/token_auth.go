package vdoc

import (
	"fmt"
	"time"

	domain "vdoc/domain/vdoc"
)

func (s *Store) authenticateMCPTokenRepository(repo domain.MCPTokenAuthRepository, secret string, ctx AuditContext) (*MCPToken, *User, error) {
	var authenticated *MCPToken
	var actor *User
	err := repo.UseMCPToken(s.requestContext(), sha(secret), func(token *MCPToken, user *User) *AuditLog {
		now := time.Now()
		ctx.ActorType, ctx.ActorTokenID = AuditActorMCPToken, token.ID
		metadata := auditMetadata("result", "failure", "token_id", token.ID)
		switch {
		case expireMCPTokenIfNeeded(token, now):
			metadata["reason"] = "expired"
		case token.Status != MCPTokenStatusActive:
			metadata["reason"], metadata["status"] = "inactive", fmt.Sprint(token.Status)
		case user == nil || user.Status != UserStatusActive:
			metadata["reason"] = "user_inactive"
		default:
			// 使用时间不参与生命周期的乐观锁版本。
			token.LastUsedAt = &now
			metadata["result"] = "success"
			authenticated, actor = cloneToken(token), cloneUser(user)
		}
		return appendAuditToState(map[string]*AuditLog{}, ctx, AuditActorMCPToken, token.UserID, "mcp_token.authenticate", "mcp_token", token.ID, "", "", metadata)
	})
	if err != nil {
		return nil, nil, err
	}
	if authenticated == nil {
		return nil, nil, ErrUnauthenticated
	}
	return authenticated, actor, nil
}
