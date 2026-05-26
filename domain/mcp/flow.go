package mcp

import (
	"time"

	commonvdoc "vdoc/common/vdoc"
)

func ExpireIfNeeded(token *MCPToken, now time.Time) bool {
	if token == nil || token.Status != commonvdoc.MCPTokenStatusActive || token.ExpiresAt == nil || now.Before(*token.ExpiresAt) {
		return false
	}
	token.Status = commonvdoc.MCPTokenStatusExpired
	token.UpdatedAt = now
	return true
}
