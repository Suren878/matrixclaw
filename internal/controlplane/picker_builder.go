package controlplane

import "strings"

type PickerBuilder struct {
	data PickerData
}

func NewPickerData(kind PickerKind, title string) *PickerBuilder {
	return &PickerBuilder{data: PickerData{Kind: kind, Title: title}}
}

func (b *PickerBuilder) Context(id string) *PickerBuilder {
	b.data.ContextID = id
	return b
}

func (b *PickerBuilder) Meta(meta string) *PickerBuilder {
	b.data.Meta = strings.TrimSpace(meta)
	return b
}

func (b *PickerBuilder) Back(command string) *PickerBuilder {
	b.data.BackCommand = command
	b.data.HasBack = true
	return b
}

func (b *PickerBuilder) Cancel(command string) *PickerBuilder {
	b.data.CancelCommand = command
	b.data.HasCancel = true
	return b
}

func (b *PickerBuilder) Select(cancelCommand string) *PickerBuilder {
	b.data.Popup = true
	b.data.Select = true
	b.data.CancelCommand = cancelCommand
	b.data.HasCancel = true
	return b
}

func (b *PickerBuilder) Popup() *PickerBuilder {
	b.data.Popup = true
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

func (b *PickerBuilder) Build() PickerData {
	data := b.data
	if len(data.Items) > 0 {
		data.Items = append([]PickerItem(nil), data.Items...)
	}
	return data
}

func (b *PickerBuilder) Ptr() *PickerData {
	data := b.Build()
	return &data
}
