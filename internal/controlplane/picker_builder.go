package controlplane

import "strings"

type PickerBuilder struct {
	data PickerData
}

func NewPickerData(kind PickerKind, title string) *PickerBuilder {
	return &PickerBuilder{data: PickerData{Kind: kind, Title: title}}
}

// Command is the command that shows this picker again.
func (b *PickerBuilder) Command(command string) *PickerBuilder {
	b.data.Command = command
	return b
}

func (b *PickerBuilder) Meta(meta string) *PickerBuilder {
	b.data.Meta = strings.TrimSpace(meta)
	return b
}

func (b *PickerBuilder) Back(command string) *PickerBuilder {
	b.data.Back = command
	return b
}

// Select makes the picker a choice that runs closeCommand when dismissed.
func (b *PickerBuilder) Select(closeCommand string) *PickerBuilder {
	b.data.Popup = true
	b.data.Close = closeCommand
	return b
}

func (b *PickerBuilder) Item(item PickerItem) *PickerBuilder {
	b.data.Items = append(b.data.Items, item)
	return b
}

func (b *PickerBuilder) Items(items ...PickerItem) *PickerBuilder {
	b.data.Items = append(b.data.Items, items...)
	return b
}

func (b *PickerBuilder) Row(id string, title string, info string, command string) *PickerBuilder {
	return b.Item(PickerItem{ID: id, Title: title, Info: info, Command: command})
}

func (b *PickerBuilder) Action(id string, title string, info string, command string) *PickerBuilder {
	return b.Item(PickerItem{ID: id, Title: title, Info: info, Command: command, Role: PickerItemRoleAction})
}

func (b *PickerBuilder) Danger(id string, title string, info string, command string) *PickerBuilder {
	return b.Item(PickerItem{ID: id, Title: title, Info: info, Command: command, Role: PickerItemRoleDanger})
}

func (b *PickerBuilder) Static(id string, title string, info string) *PickerBuilder {
	return b.Item(PickerItem{ID: id, Title: title, Info: info, Disabled: true})
}

func (b *PickerBuilder) Ptr() *PickerData {
	data := b.data
	return &data
}
