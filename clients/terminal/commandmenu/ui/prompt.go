package commandui

type PromptData struct {
	Title       string
	Value       string
	Placeholder string
	Error       string
}

func RenderPromptCard(frame Frame, data PromptData) string {
	frame = frame.WithInnerWidth(0)
	return frame.RenderCard(FrameData{
		Title: data.Title,
		Body: []string{RenderTextField(frame, TextFieldData{
			Value:       data.Value,
			Placeholder: data.Placeholder,
			Inset:       1,
			Active:      true,
		})},
		Help:  "enter save · esc cancel",
		Error: data.Error,
	})
}
