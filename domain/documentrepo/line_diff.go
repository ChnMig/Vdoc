package documentrepo

import "sort"

// LineChange 的起点是从零开始的行偏移；行内容是否保留换行由调用方决定。
type LineChange struct {
	OldStart int
	NewStart int
	OldLines []string
	NewLines []string
}

type lineAnchor struct {
	from int
	to   int
}

const (
	maxSourceDiffDepth = 64
	maxSourceLCSCells  = 256 * 1024
)

// ChangedLines 使用有界 patience/LCS 比较，不为无关大文件分配平方级内存。
func ChangedLines(oldLines, newLines []string) []LineChange {
	blocks := []LineChange{}
	collectLineChanges(oldLines, newLines, 0, len(oldLines), 0, len(newLines), 0, &blocks)
	return blocks
}

func collectLineChanges(fromLines, toLines []string, fromStart, fromEnd, toStart, toEnd, depth int, blocks *[]LineChange) {
	for fromStart < fromEnd && toStart < toEnd && fromLines[fromStart] == toLines[toStart] {
		fromStart++
		toStart++
	}
	for fromStart < fromEnd && toStart < toEnd && fromLines[fromEnd-1] == toLines[toEnd-1] {
		fromEnd--
		toEnd--
	}
	if fromStart == fromEnd && toStart == toEnd {
		return
	}
	if depth >= maxSourceDiffDepth {
		appendLineChange(fromLines, toLines, fromStart, fromEnd, toStart, toEnd, blocks)
		return
	}
	anchors := sourcePatienceAnchors(fromLines, toLines, fromStart, fromEnd, toStart, toEnd)
	if len(anchors) == 0 {
		anchors = sourceLCSAnchors(fromLines, toLines, fromStart, fromEnd, toStart, toEnd)
		if len(anchors) == 0 {
			appendLineChange(fromLines, toLines, fromStart, fromEnd, toStart, toEnd, blocks)
			return
		}
	}
	previousFrom, previousTo := fromStart, toStart
	for _, anchor := range anchors {
		collectLineChanges(fromLines, toLines, previousFrom, anchor.from, previousTo, anchor.to, depth+1, blocks)
		previousFrom, previousTo = anchor.from+1, anchor.to+1
	}
	collectLineChanges(fromLines, toLines, previousFrom, fromEnd, previousTo, toEnd, depth+1, blocks)
}

// sourceLCSAnchors recovers useful matches from small blocks made entirely
// of repeated lines, where patience diff intentionally has no unique anchors.
// The bounded cell count keeps adversarial or very large unrelated documents
// on the existing linear-memory fallback path.
func sourceLCSAnchors(fromLines, toLines []string, fromStart, fromEnd, toStart, toEnd int) []lineAnchor {
	fromCount := fromEnd - fromStart
	toCount := toEnd - toStart
	if fromCount == 0 || toCount == 0 {
		return nil
	}
	rows, columns := fromCount+1, toCount+1
	if rows > maxSourceLCSCells/columns {
		return nil
	}
	table := make([]uint32, rows*columns)
	cell := func(row, column int) int { return row*columns + column }
	for fromIndex := fromCount - 1; fromIndex >= 0; fromIndex-- {
		for toIndex := toCount - 1; toIndex >= 0; toIndex-- {
			if fromLines[fromStart+fromIndex] == toLines[toStart+toIndex] {
				table[cell(fromIndex, toIndex)] = table[cell(fromIndex+1, toIndex+1)] + 1
				continue
			}
			remove := table[cell(fromIndex+1, toIndex)]
			add := table[cell(fromIndex, toIndex+1)]
			if remove >= add {
				table[cell(fromIndex, toIndex)] = remove
			} else {
				table[cell(fromIndex, toIndex)] = add
			}
		}
	}

	anchors := make([]lineAnchor, 0, int(table[0]))
	for fromIndex, toIndex := 0, 0; fromIndex < fromCount && toIndex < toCount; {
		if fromLines[fromStart+fromIndex] == toLines[toStart+toIndex] {
			anchors = append(anchors, lineAnchor{from: fromStart + fromIndex, to: toStart + toIndex})
			fromIndex++
			toIndex++
			continue
		}
		if table[cell(fromIndex+1, toIndex)] >= table[cell(fromIndex, toIndex+1)] {
			fromIndex++
		} else {
			toIndex++
		}
	}
	return anchors
}

func appendLineChange(fromLines, toLines []string, fromStart, fromEnd, toStart, toEnd int, blocks *[]LineChange) {
	if fromStart == fromEnd && toStart == toEnd {
		return
	}
	*blocks = append(*blocks, LineChange{
		OldStart: fromStart,
		NewStart: toStart,
		OldLines: append([]string(nil), fromLines[fromStart:fromEnd]...),
		NewLines: append([]string(nil), toLines[toStart:toEnd]...),
	})
}

func sourcePatienceAnchors(fromLines, toLines []string, fromStart, fromEnd, toStart, toEnd int) []lineAnchor {
	fromCounts := make(map[string]int, fromEnd-fromStart)
	toCounts := make(map[string]int, toEnd-toStart)
	toIndexes := make(map[string]int, toEnd-toStart)
	for index := fromStart; index < fromEnd; index++ {
		fromCounts[fromLines[index]]++
	}
	for index := toStart; index < toEnd; index++ {
		line := toLines[index]
		toCounts[line]++
		toIndexes[line] = index
	}
	candidates := make([]lineAnchor, 0)
	for index := fromStart; index < fromEnd; index++ {
		line := fromLines[index]
		if fromCounts[line] == 1 && toCounts[line] == 1 {
			candidates = append(candidates, lineAnchor{from: index, to: toIndexes[line]})
		}
	}
	if len(candidates) < 2 {
		return candidates
	}
	tails := make([]int, 0, len(candidates))
	tailCandidate := make([]int, 0, len(candidates))
	previous := make([]int, len(candidates))
	for index := range previous {
		previous[index] = -1
	}
	for candidateIndex, candidate := range candidates {
		position := sort.Search(len(tails), func(index int) bool { return tails[index] >= candidate.to })
		if position == len(tails) {
			tails = append(tails, candidate.to)
			tailCandidate = append(tailCandidate, candidateIndex)
		} else {
			tails[position] = candidate.to
			tailCandidate[position] = candidateIndex
		}
		if position > 0 {
			previous[candidateIndex] = tailCandidate[position-1]
		}
	}
	anchors := make([]lineAnchor, len(tails))
	for index, candidateIndex := len(anchors)-1, tailCandidate[len(tailCandidate)-1]; index >= 0; index-- {
		anchors[index] = candidates[candidateIndex]
		candidateIndex = previous[candidateIndex]
	}
	return anchors
}
