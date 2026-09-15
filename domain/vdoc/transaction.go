package vdoc

import "errors"

// ErrCommitOutcomeUnknown 表示业务写入已执行，但数据库未确认 COMMIT 结果；不得据此删除可能已提交的正文。
var ErrCommitOutcomeUnknown = errors.New("transaction commit outcome is unknown")
