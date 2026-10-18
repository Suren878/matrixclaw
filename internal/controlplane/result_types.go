package controlplane

// Screen is what a client shows: TextData, PickerData, FormData,
// PromptData, ConfirmData or InfoData.
type Screen interface{ screen() }

func (TextData) screen()    {}
func (PickerData) screen()  {}
func (FormData) screen()    {}
func (PromptData) screen()  {}
func (ConfirmData) screen() {}
func (InfoData) screen()    {}

// TextData is a plain answer.
type TextData struct {
	Text string
}

// PromptData asks for one value, which the client hands to
// Dispatcher.Answer with Submit; Multiline asks for a text to edit rather
// than a line, and Sensitive for a secret the client neither shows nor keeps.
type PromptData struct {
	Title         string
	Placeholder   string
	Value         string
	Submit        string
	CancelCommand string
	Sensitive     bool
	Multiline     bool
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
	Title        string
	Text         string
	Rows         []InfoRow
	CloseCommand string
}

type InfoRow struct {
	Label string
	Value string
}

func deleteConfirmData(message string, confirmCommand string, cancelCommand string) ConfirmData {
	return ConfirmData{
		Message:        message,
		ConfirmLabel:   "Delete",
		CancelLabel:    "Close",
		ConfirmCommand: confirmCommand,
		CancelCommand:  cancelCommand,
		ConfirmDanger:  true,
	}
}
