package components

func selectableCount(items []Item) int {
	count := 0
	for _, item := range items {
		if !item.Selectable() || item.Role == RoleBack || item.Role == RoleCancel || item.Role == RoleExit {
			continue
		}
		count++
	}
	return count
}
