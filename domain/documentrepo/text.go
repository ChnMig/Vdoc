package documentrepo

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	domain "vdoc/domain/vdoc"
)

const SectionByteLimit = 256 << 10
const SectionLineLimit = 500

type TextEdit struct {
	OldText string `json:"old_text"`
	NewText string `json:"new_text"`
}

// ApplyTextEdits 的所有锚点都来自同一份原文；不把前一个替换的结果当成后一个锚点。
func ApplyTextEdits(content string, edits []TextEdit) (string, error) {
	if len(edits) == 0 || len(edits) > 100 {
		return "", fmt.Errorf("%w: edits must contain 1 to 100 replacements", domain.ErrInvalidArgument)
	}
	type replacement struct {
		start, end int
		text       string
	}
	changes := make([]replacement, 0, len(edits))
	for _, edit := range edits {
		if !utf8.ValidString(edit.OldText) || !utf8.ValidString(edit.NewText) {
			return "", fmt.Errorf("%w: edits must be valid UTF-8", domain.ErrInvalidArgument)
		}
		if edit.OldText == "" {
			if content != "" || len(edits) != 1 {
				return "", fmt.Errorf("%w: an empty anchor is only valid for an empty document with one edit", domain.ErrInvalidArgument)
			}
			return edit.NewText, nil
		}
		start := strings.Index(content, edit.OldText)
		if start < 0 || strings.Contains(content[start+1:], edit.OldText) {
			return "", fmt.Errorf("%w: PATCH_CONFLICT: each old_text must match exactly once in the original content; read the exact commit again", domain.ErrFailedPrecondition)
		}
		changes = append(changes, replacement{start, start + len(edit.OldText), edit.NewText})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].start < changes[j].start })
	var out strings.Builder
	previous := 0
	for _, change := range changes {
		if change.start < previous {
			return "", fmt.Errorf("%w: edits overlap in the original content", domain.ErrInvalidArgument)
		}
		out.WriteString(content[previous:change.start])
		out.WriteString(change.text)
		previous = change.end
	}
	out.WriteString(content[previous:])
	return out.String(), nil
}

type TextSection struct {
	Content    string
	StartLine  int
	EndLine    int
	TotalLines int
	NextLine   int
}

// SourceLines 保留 BOM、CRLF、末尾换行，不为结尾换行伪造一个额外空行。
func SourceLines(content string) []string {
	if content == "" {
		return nil
	}
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func ReadTextLines(content string, start, end int) (TextSection, error) {
	lines := SourceLines(content)
	out := TextSection{TotalLines: len(lines)}
	if start == 0 {
		start = 1
	}
	if len(lines) == 0 && start == 1 && end == 0 {
		return out, nil
	}
	if start < 1 || start > len(lines) || end < 0 || end != 0 && end < start {
		return out, fmt.Errorf("%w: invalid 1-based inclusive line range", domain.ErrInvalidArgument)
	}
	if end == 0 {
		end = min(start+199, len(lines))
	}
	if end > len(lines) || end-start+1 > SectionLineLimit {
		return out, fmt.Errorf("%w: select at most 500 existing lines", domain.ErrInvalidArgument)
	}
	length := 0
	for _, line := range lines[start-1 : end] {
		length += len(line)
	}
	if length > SectionByteLimit {
		return out, fmt.Errorf("%w: SECTION_LIMIT: use a smaller range or get_document_at_commit for the complete source", domain.ErrFailedPrecondition)
	}
	out.Content, out.StartLine, out.EndLine = strings.Join(lines[start-1:end], ""), start, end
	if end < len(lines) {
		out.NextLine = end + 1
	}
	return out, nil
}

type MarkdownHeading struct {
	Heading   string `json:"heading"`
	Level     int    `json:"level"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

// MarkdownHeadings 识别围栏代码块以外的 ATX 标题。其他 Markdown 语法仍可按行或完整原文读取。
func MarkdownHeadings(content string) []MarkdownHeading {
	lines := SourceLines(content)
	headings := []MarkdownHeading{}
	var fence byte
	fenceLength := 0
	for i, source := range lines {
		line := strings.TrimSuffix(strings.TrimSuffix(source, "\n"), "\r")
		if i == 0 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent > 3 {
			continue
		}
		line = line[indent:]
		if len(line) > 0 && (line[0] == '`' || line[0] == '~') {
			count := len(line) - len(strings.TrimLeft(line, string(line[0])))
			if count >= 3 && fence == 0 && (line[0] != '`' || !strings.Contains(line[count:], "`")) {
				fence, fenceLength = line[0], count
				continue
			}
			if fence == line[0] && count >= fenceLength && strings.TrimSpace(line[count:]) == "" {
				fence = 0
				continue
			}
		}
		if fence != 0 {
			continue
		}
		level := len(line) - len(strings.TrimLeft(line, "#"))
		if level < 1 || level > 6 || len(line) > level && line[level] != ' ' && line[level] != '\t' {
			continue
		}
		title := strings.TrimSpace(line[level:])
		trimmed := strings.TrimRight(title, "#")
		if len(trimmed) < len(title) && (trimmed == "" || strings.HasSuffix(trimmed, " ") || strings.HasSuffix(trimmed, "\t")) {
			title = strings.TrimSpace(trimmed)
		}
		headings = append(headings, MarkdownHeading{Heading: title, Level: level, StartLine: i + 1, EndLine: len(lines)})
	}
	for i := range headings {
		for j := i + 1; j < len(headings); j++ {
			if headings[j].Level <= headings[i].Level {
				headings[i].EndLine = headings[j].StartLine - 1
				break
			}
		}
	}
	return headings
}

func ReadMarkdownHeading(content, heading string) (TextSection, error) {
	var selected *MarkdownHeading
	for _, item := range MarkdownHeadings(content) {
		if item.Heading == heading {
			if selected != nil {
				return TextSection{}, fmt.Errorf("%w: heading is ambiguous; select an exact line range", domain.ErrInvalidArgument)
			}
			copy := item
			selected = &copy
		}
	}
	if selected == nil {
		return TextSection{}, domain.ErrNotFound
	}
	return ReadTextLines(content, selected.StartLine, selected.EndLine)
}
