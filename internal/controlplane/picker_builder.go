package controlplane

import "strings"

type PickerBuilder struct {
	data             PickerData
	autoCommandItems map[int]struct{}
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
	b.data.CloseCommand = command
	b.data.HasBack = true
	b.data.HasClose = true
	return b
}

func (b *PickerBuilder) Close(command string) *PickerBuilder {
	b.data.CloseCommand = command
	b.data.HasClose = true
	return b
}

func (b *PickerBuilder) HideBack(hide bool) *PickerBuilder {
	b.data.HideBackItem = hide
	return b
}

func (b *PickerBuilder) Item(item PickerItem) *PickerBuilder {
	return b.item(item, false)
}

func (b *PickerBuilder) Items(items ...PickerItem) *PickerBuilder {
	b.data.Items = append(b.data.Items, items...)
	return b
}

func (b *PickerBuilder) Row(id string, title string, info string, command ...string) *PickerBuilder {
	item := PickerItem{ID: id, Title: title, Info: info}
	explicit := applyPickerBuilderCommand(&item, command)
	return b.item(item, !explicit)
}

func (b *PickerBuilder) Action(id string, title string, info string, command ...string) *PickerBuilder {
	item := PickerItem{ID: id, Title: title, Info: info, Role: PickerItemRoleAction}
	explicit := applyPickerBuilderCommand(&item, command)
	return b.item(item, !explicit)
}

func (b *PickerBuilder) Danger(id string, title string, info string, command ...string) *PickerBuilder {
	item := PickerItem{ID: id, Title: title, Info: info, Role: PickerItemRoleDanger}
	explicit := applyPickerBuilderCommand(&item, command)
	return b.item(item, !explicit)
}

func (b *PickerBuilder) CloseItem(label ...string) *PickerBuilder {
	return b.Item(CloseItem(label...))
}

func (b *PickerBuilder) Build() PickerData {
	data := b.data
	if len(data.Items) > 0 {
		data.Items = append([]PickerItem(nil), data.Items...)
	}
	for index := range b.autoCommandItems {
		if index < 0 || index >= len(data.Items) {
			continue
		}
		item := &data.Items[index]
		if strings.TrimSpace(item.Command) != "" || item.IsNavigation() {
			continue
		}
		item.Command = PickerCommandFor(data.Kind, data.ContextID, item.ID)
	}
	if shouldAppendBackItem(data) {
		data.Items = append(data.Items, BackItem())
	}
	return data
}

func (b *PickerBuilder) Ptr() *PickerData {
	data := b.Build()
	return &data
}

func (b *PickerBuilder) item(item PickerItem, autoCommand bool) *PickerBuilder {
	index := len(b.data.Items)
	b.data.Items = append(b.data.Items, item)
	if autoCommand {
		if b.autoCommandItems == nil {
			b.autoCommandItems = map[int]struct{}{}
		}
		b.autoCommandItems[index] = struct{}{}
	}
	return b
}

func applyPickerBuilderCommand(item *PickerItem, command []string) bool {
	if len(command) == 0 {
		return false
	}
	item.Command = command[0]
	return true
}

func hasNavigationItem(items []PickerItem) bool {
	for _, item := range items {
		if item.IsNavigation() {
			return true
		}
	}
	return false
}

func shouldAppendBackItem(data PickerData) bool {
	return !data.HideBackItem && strings.TrimSpace(data.BackCommand) != "" && !hasNavigationItem(data.Items)
}
