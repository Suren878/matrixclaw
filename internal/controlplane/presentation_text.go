package controlplane

import "strings"

func PickerPresentationText(picker PickerData) string {
	title := strings.TrimSpace(picker.Title)
	if title == "" {
		return "Choose:"
	}
	return title
}

func FormPresentationText(form FormData) string {
	title := strings.TrimSpace(form.Title)
	if title == "" {
		title = "Form"
	}
	lines := []string{title}
	for _, field := range form.Fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" {
			continue
		}
		if value == "" {
			value = "Empty"
		}
		lines = append(lines, label+": "+value)
	}
	return strings.Join(lines, "\n")
}

func ConfirmPresentationText(confirm ConfirmData) string {
	text := strings.TrimSpace(confirm.Message)
	if title := strings.TrimSpace(confirm.Title); title != "" {
		if text == "" {
			return title
		}
		return title + "\n\n" + text
	}
	return text
}

func InfoPresentationText(info InfoData) string {
	title := strings.TrimSpace(info.Title)
	lines := []string{}
	if title != "" {
		lines = append(lines, title)
	}
	for _, row := range info.Rows {
		label := strings.TrimSpace(row.Label)
		value := strings.TrimSpace(row.Value)
		switch {
		case label != "" && value != "":
			lines = append(lines, label+": "+value)
		case label != "":
			lines = append(lines, label)
		case value != "":
			lines = append(lines, value)
		}
	}
	if text := strings.TrimSpace(info.Text); text != "" {
		lines = append(lines, text)
	}
	return strings.Join(lines, "\n")
}
