package vdoc

import "context"

// HistoryReadRepository 的列表不包含正文、预览或差异明细。
type HistoryReadRepository interface {
	ReadDraftPage(context.Context, string, string, PageQuery) ([]*ContractDraft, int, error)
	ReadDiffPage(context.Context, string, string, string, PageQuery) ([]*Diff, int, error)
}
