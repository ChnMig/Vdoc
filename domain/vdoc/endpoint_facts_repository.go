package vdoc

import "context"

// EndpointFactsRepository 仅升级可重建索引，不修改已发布原文、内容哈希或对象引用。
type EndpointFactsRepository interface {
	RefreshEndpointFacts(context.Context, string, string, int, string, []Endpoint) error
}
