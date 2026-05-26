package documentdiff

func CountBreaking(items []DiffItem) int {
	count := 0
	for _, item := range items {
		if item.IsBreaking {
			count++
		}
	}
	return count
}
