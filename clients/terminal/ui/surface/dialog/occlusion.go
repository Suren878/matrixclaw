package dialog

func (*Picker) OccludesBelow() bool {
	return true
}

func (*ConfirmCommand) OccludesBelow() bool {
	return true
}

func (*ConfirmRunCancel) OccludesBelow() bool {
	return true
}

func (*PromptCommand) OccludesBelow() bool {
	return true
}

func (*TextEditCommand) OccludesBelow() bool {
	return true
}

func (*FormCommand) OccludesBelow() bool {
	return true
}

func (*Info) OccludesBelow() bool {
	return true
}

func (*Permissions) OccludesBelow() bool {
	return true
}

func (*DiffPreview) OccludesBelow() bool {
	return true
}

func (*FilePreview) OccludesBelow() bool {
	return true
}
