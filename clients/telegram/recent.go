package telegram

// recentMap keeps the newest limit keys and forgets the oldest ones.
type recentMap[V any] struct {
	limit  int
	values map[string]V
	order  []string
}

func newRecentMap[V any](limit int) *recentMap[V] {
	return &recentMap[V]{limit: limit, values: map[string]V{}}
}

func (m *recentMap[V]) get(key string) (V, bool) {
	value, ok := m.values[key]
	return value, ok
}

// put stores value and reports whether key was already present.
func (m *recentMap[V]) put(key string, value V) bool {
	_, existed := m.values[key]
	m.values[key] = value
	if existed {
		return true
	}
	m.order = append(m.order, key)
	if len(m.order) > m.limit {
		delete(m.values, m.order[0])
		m.order = m.order[1:]
	}
	return false
}

func (m *recentMap[V]) snapshot() map[string]V {
	out := make(map[string]V, len(m.values))
	for key, value := range m.values {
		out[key] = value
	}
	return out
}
