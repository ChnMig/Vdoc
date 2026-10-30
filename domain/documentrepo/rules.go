package documentrepo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	common "vdoc/common/vdoc"
)

func Hash(value []byte) string { sum := sha256.Sum256(value); return hex.EncodeToString(sum[:]) }

func EqualFile(a, b File) bool {
	a.ObjectKey, b.ObjectKey = "", ""
	return a == b
}

func TreeHash(files []File) string {
	canonical := append([]File{}, files...)
	sort.Slice(canonical, func(i, j int) bool { return canonical[i].RelativePath < canonical[j].RelativePath })
	for i := range canonical {
		canonical[i].ObjectKey = ""
	}
	raw, _ := json.Marshal(canonical)
	return Hash(raw)
}

func NewCommit(projectID, author, message string, parents []string, files []File, now time.Time) *Commit {
	files = append([]File{}, files...)
	sort.Slice(files, func(i, j int) bool { return files[i].RelativePath < files[j].RelativePath })
	c := &Commit{ProjectID: projectID, ParentIDs: append([]string{}, parents...), TreeHash: TreeHash(files), CreatedBy: author, CreatedAt: now.UTC().Truncate(time.Microsecond), Message: strings.TrimSpace(message), Files: files}
	identity, _ := json.Marshal([]any{c.ProjectID, c.ParentIDs, c.TreeHash, c.CreatedBy, c.CreatedAt, c.Message})
	c.ID = Hash(identity)
	return c
}

func ValidateFiles(files []File) error {
	if len(files) > 5000 {
		return fmt.Errorf("%w: repository file limit exceeded", common.ErrInvalidArgument)
	}
	paths, ids := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		if f.DocumentID == "" || len(f.DocumentID) > 256 || strings.ContainsAny(f.DocumentID, "\x00\r\n") || strings.TrimSpace(f.Name) == "" || len(f.Name) > 200 || len(f.Description) > 4000 || len(f.RelativePath) > 4096 || !utf8.ValidString(f.DocumentID) || !utf8.ValidString(f.Name) || !utf8.ValidString(f.Description) || !utf8.ValidString(f.RelativePath) || strings.ContainsAny(f.RelativePath, "\\\x00") || strings.HasPrefix(f.RelativePath, "/") || f.RelativePath == "." || path.Clean(f.RelativePath) != f.RelativePath || strings.HasPrefix(f.RelativePath, "../") {
			return fmt.Errorf("%w: invalid document identity or relative path", common.ErrInvalidArgument)
		}
		if err := common.ValidateDocumentRelativePath(f.DocumentType, f.RelativePath); err != nil {
			return fmt.Errorf("%w: %v", common.ErrInvalidArgument, err)
		}
		if paths[f.RelativePath] || ids[f.DocumentID] {
			return fmt.Errorf("%w: duplicate path or document identity", common.ErrAlreadyExists)
		}
		paths[f.RelativePath], ids[f.DocumentID] = true, true
	}
	return nil
}

// MergeText 合并基于同一原文的不相交行修改，保留原始换行字节。
func MergeText(base, left, right string) (string, bool) {
	if left == right || right == base {
		return left, true
	}
	if left == base {
		return right, true
	}
	b, l, r := strings.SplitAfter(base, "\n"), strings.SplitAfter(left, "\n"), strings.SplitAfter(right, "\n")
	le, ok := edits(b, l)
	if !ok {
		return "", false
	}
	re, ok := edits(b, r)
	if !ok {
		return "", false
	}
	all := append(le, re...)
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].start == all[j].start {
			return all[i].end < all[j].end
		}
		return all[i].start < all[j].start
	})
	merged := []lineEdit{}
	for _, e := range all {
		if len(merged) > 0 {
			p := merged[len(merged)-1]
			if p.start == e.start && p.end == e.end && strings.Join(p.lines, "") == strings.Join(e.lines, "") {
				continue
			}
			if e.start < p.end || (e.start == p.start && (e.start == e.end || p.start == p.end)) {
				return "", false
			}
		}
		merged = append(merged, e)
	}
	var out strings.Builder
	position := 0
	for _, e := range merged {
		out.WriteString(strings.Join(b[position:e.start], ""))
		out.WriteString(strings.Join(e.lines, ""))
		position = e.end
	}
	out.WriteString(strings.Join(b[position:], ""))
	return out.String(), true
}

type lineEdit struct {
	start, end int
	lines      []string
}

func edits(a, b []string) ([]lineEdit, bool) {
	if int64(len(a)+1)*int64(len(b)+1) > 4_000_000 {
		return nil, false
	}
	w := len(b) + 1
	lcs := make([]int, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}
	i, j := 0, 0
	result := []lineEdit{}
	for i < len(a) || j < len(b) {
		if i < len(a) && j < len(b) && a[i] == b[j] {
			i++
			j++
			continue
		}
		e := lineEdit{start: i, end: i, lines: []string{}}
		for i < len(a) || j < len(b) {
			if i < len(a) && j < len(b) && a[i] == b[j] {
				break
			}
			if j < len(b) && (i == len(a) || lcs[i*w+j+1] >= lcs[(i+1)*w+j]) {
				e.lines = append(e.lines, b[j])
				j++
			} else {
				i++
				e.end = i
			}
		}
		result = append(result, e)
	}
	return result, true
}
