package model

type PromptHistory struct {
	messages []string
	index    int
	draft    string
}

func NewPromptHistory() PromptHistory {
	return PromptHistory{index: -1}
}

func (h *PromptHistory) SetMessages(messages []string) {
	h.messages = append(h.messages[:0], messages...)
	if h.index >= len(h.messages) {
		h.index = -1
	}
}

func (h *PromptHistory) Messages() []string {
	return append([]string(nil), h.messages...)
}

func (h *PromptHistory) Index() int {
	return h.index
}

func (h *PromptHistory) Draft() string {
	return h.draft
}

func (h *PromptHistory) UpdateDraft(previous, current string) {
	if current != previous {
		h.draft = current
		h.index = -1
	}
}

func (h *PromptHistory) Prev(current string) (string, bool) {
	if len(h.messages) == 0 {
		return "", false
	}
	if h.index == -1 {
		h.draft = current
	}
	nextIndex := h.index + 1
	if nextIndex >= len(h.messages) {
		return "", false
	}
	h.index = nextIndex
	return h.messages[nextIndex], true
}

func (h *PromptHistory) Next() (string, bool) {
	if h.index < 0 {
		return "", false
	}
	nextIndex := h.index - 1
	if nextIndex < 0 {
		h.index = -1
		return h.draft, true
	}
	h.index = nextIndex
	return h.messages[nextIndex], true
}

func (h *PromptHistory) RestoreDraft() (string, bool) {
	if h.index < 0 {
		return "", false
	}
	h.index = -1
	return h.draft, true
}

func (h *PromptHistory) Reset() {
	h.index = -1
	h.draft = ""
}
