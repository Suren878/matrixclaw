package controlplane

type PromptData struct {
	Title               string
	Placeholder         string
	Value               string
	SubmitCommandPrefix string
	CancelCommand       string
	Sensitive           bool
}

type TextEditData struct {
	Title               string
	Placeholder         string
	Value               string
	SubmitCommandPrefix string
	CancelCommand       string
}

type FormData struct {
	Title         string
	Fields        []FormField
	SubmitLabel   string
	CancelLabel   string
	SubmitCommand string
	CancelCommand string
	Error         string
}

type FormField struct {
	ID          string
	Label       string
	Value       string
	EditCommand string
	Disabled    bool
}

type ConfirmData struct {
	Title          string
	Message        string
	ConfirmLabel   string
	CancelLabel    string
	ConfirmCommand string
	CancelCommand  string
	ConfirmDanger  bool
	CancelDanger   bool
}

type InfoData struct {
	Title         string
	Text          string
	Rows          []InfoRow
	CancelCommand string
}

type InfoRow struct {
	Label string
	Value string
}

func deleteConfirmData(message string, confirmCommand string, cancelCommand string) *ConfirmData {
	return &ConfirmData{
		Message:        message,
		ConfirmLabel:   "Delete",
		CancelLabel:    "Cancel",
		ConfirmCommand: confirmCommand,
		CancelCommand:  cancelCommand,
		ConfirmDanger:  true,
	}
}
