package documentrepo

import "strings"

const SourceDiffByteLimit = 64 << 10
const SourceDiffHunkLimit = 20
const sourceDiffContextLines = 3

type SourceHunk struct {
	OldStartLine, OldLineCount int
	NewStartLine, NewLineCount int
	OldContent, NewContent     string
	ContentOmitted             bool
}

type SourceDiff struct {
	Hunks                    []SourceHunk
	AddedLines, RemovedLines int
	HunkCount                int
	Truncated                bool
}

// CompareSource 保留原始换行和空白，预览使用有界正文预算；省略时仍返回完整行范围。
func CompareSource(before, after string, byteLimit int) (SourceDiff, int) {
	oldLines, newLines := SourceLines(before), SourceLines(after)
	blocks := ChangedLines(oldLines, newLines)
	out := SourceDiff{Hunks: []SourceHunk{}}
	type span struct{ oldStart, oldEnd, newStart, newEnd int }
	spans := []span{}
	for _, block := range blocks {
		out.RemovedLines += len(block.OldLines)
		out.AddedLines += len(block.NewLines)
		current := span{
			max(0, block.OldStart-sourceDiffContextLines), min(len(oldLines), block.OldStart+len(block.OldLines)+sourceDiffContextLines),
			max(0, block.NewStart-sourceDiffContextLines), min(len(newLines), block.NewStart+len(block.NewLines)+sourceDiffContextLines),
		}
		if n := len(spans); n > 0 && current.oldStart <= spans[n-1].oldEnd && current.newStart <= spans[n-1].newEnd {
			spans[n-1].oldEnd, spans[n-1].newEnd = current.oldEnd, current.newEnd
		} else {
			spans = append(spans, current)
		}
	}
	out.HunkCount = len(spans)
	used := 0
	for _, span := range spans {
		if len(out.Hunks) == SourceDiffHunkLimit {
			out.Truncated = true
			break
		}
		hunk := SourceHunk{OldStartLine: span.oldStart + 1, OldLineCount: span.oldEnd - span.oldStart, NewStartLine: span.newStart + 1, NewLineCount: span.newEnd - span.newStart}
		size := 0
		for _, line := range oldLines[span.oldStart:span.oldEnd] {
			size += len(line)
		}
		for _, line := range newLines[span.newStart:span.newEnd] {
			size += len(line)
		}
		if size > max(0, byteLimit-used) {
			hunk.ContentOmitted, out.Truncated = true, true
		} else {
			hunk.OldContent = strings.Join(oldLines[span.oldStart:span.oldEnd], "")
			hunk.NewContent = strings.Join(newLines[span.newStart:span.newEnd], "")
			used += size
		}
		out.Hunks = append(out.Hunks, hunk)
	}
	return out, used
}
