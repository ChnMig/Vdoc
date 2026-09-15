package vdoc

import (
	"crypto/rand"
	"fmt"
)

func newObjectWriteKey(projectID, documentID, branchID, ownerType, ownerID, kind, hash, extension string) string {
	// 每次写入独占一个随机路径，避免相同内容的并发写入或失败回滚覆盖、删除已提交对象。
	// 内容哈希仍保留在文件名和元数据中；旧对象按数据库记录的完整键读取，无需迁移。
	return fmt.Sprintf("projects/%s/documents/%s/branches/%s/%ss/%s/%s/%s-%s.%s", projectID, documentID, branchID, ownerType, ownerID, rand.Text(), kind, hash, extension)
}
