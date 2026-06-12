package controlplane

import "strings"

type Surface string

const (
	SurfaceTerminal            Surface = "terminal"
	SurfaceTelegram            Surface = "telegram"
	SurfaceTelegramBotCommands Surface = "telegram_bot_commands"
)

type ScreenKind string

const (
	ScreenText     ScreenKind = "text"
	ScreenPicker   ScreenKind = "picker"
	ScreenForm     ScreenKind = "form"
	ScreenPrompt   ScreenKind = "prompt"
	ScreenTextEdit ScreenKind = "text_edit"
	ScreenConfirm  ScreenKind = "confirm"
	ScreenInfo     ScreenKind = "info"
)

type FooterKind string

const (
	FooterBack  FooterKind = "back"
	FooterClose FooterKind = "close"
)

const SelectedMarker = "✅"

type ResultViewOptions struct {
	Surface  Surface
	Page     int
	PageSize int
}

type PickerViewOptions struct {
	Surface  Surface
	Page     int
	PageSize int
}

type CommandMenuResultView struct {
	Surface Surface
	Items   []ResultViewItem
}

type PickerViewData struct {
	Kind   PickerKind
	Title  string
	Text   string
	Meta   string
	Legend string
	Items  []ResultViewItem
	Footer *ResultViewFooter
	Paging ResultViewPaging
}

type ResultView struct {
	Screen ScreenKind
	Title  string
	Text   string
	Meta   string
	Items  []ResultViewItem
	Footer *ResultViewFooter
	Paging ResultViewPaging
}

type ResultViewItem struct {
	ID              string
	Label           string
	Title           string
	Info            string
	Search          string
	Command         string
	Selected        bool
	Focused         bool
	Disabled        bool
	Role            PickerItemRole
	SeparatorBefore bool
}

type ResultViewFooter struct {
	Label   string
	Command string
	Kind    FooterKind
	Hidden  bool
}

type ResultViewPaging struct {
	Page  int
	Pages int
}

func PresentResult(result Result, options ResultViewOptions) ResultView {
	surface := normalizeSurface(options.Surface)
	switch {
	case result.Picker != nil:
		picker := PickerView(*result.Picker, PickerViewOptions{
			Surface:  surface,
			Page:     options.Page,
			PageSize: options.PageSize,
		})
		return ResultView{
			Screen: ScreenPicker,
			Title:  picker.Title,
			Text:   picker.Text,
			Meta:   picker.Meta,
			Items:  picker.Items,
			Footer: picker.Footer,
			Paging: picker.Paging,
		}
	case result.Form != nil:
		return ResultView{
			Screen: ScreenForm,
			Title:  strings.TrimSpace(result.Form.Title),
			Text:   formPresentationText(*result.Form),
		}
	case result.Prompt != nil:
		return ResultView{
			Screen: ScreenPrompt,
			Title:  strings.TrimSpace(result.Prompt.Title),
			Text:   promptResultViewText(*result.Prompt, surface),
		}
	case result.TextEdit != nil:
		return ResultView{
			Screen: ScreenTextEdit,
			Title:  strings.TrimSpace(result.TextEdit.Title),
			Text:   strings.TrimSpace(result.TextEdit.Value),
			Meta:   strings.TrimSpace(result.TextEdit.Placeholder),
			Footer: textEditViewFooter(*result.TextEdit),
		}
	case result.Confirm != nil:
		return ResultView{
			Screen: ScreenConfirm,
			Title:  strings.TrimSpace(result.Confirm.Title),
			Text:   confirmPresentationText(*result.Confirm),
		}
	case result.Info != nil:
		return ResultView{
			Screen: ScreenInfo,
			Title:  strings.TrimSpace(result.Info.Title),
			Text:   infoPresentationText(*result.Info),
			Footer: infoViewFooter(*result.Info, surface),
		}
	default:
		return ResultView{
			Screen: ScreenText,
			Text:   strings.TrimSpace(result.Text),
		}
	}
}

func CommandMenuView(surface Surface, state MenuState) CommandMenuResultView {
	surface = normalizeSurface(surface)
	views := commandViewsForSurface(surface, state)
	items := make([]ResultViewItem, 0, len(views))
	for _, view := range views {
		items = append(items, commandResultViewItem(view))
	}
	return CommandMenuResultView{Surface: surface, Items: items}
}

func PickerView(picker PickerData, options PickerViewOptions) PickerViewData {
	surface := normalizeSurface(options.Surface)
	paged := pagedPickerForView(picker, options)
	view := PickerViewData{
		Kind:   picker.Kind,
		Title:  pickerPresentationTitle(picker),
		Text:   pickerPresentationText(picker),
		Meta:   strings.TrimSpace(picker.Meta),
		Legend: pickerLegend(picker),
		Items:  pickerResultViewItems(picker, paged.Items, surface),
		Footer: pickerResultViewFooter(picker, surface),
		Paging: ResultViewPaging{Page: paged.Page, Pages: paged.Pages},
	}
	return view
}

func promptPresentationText(prompt PromptData) string {
	text := strings.TrimSpace(prompt.Title)
	if text == "" {
		text = "Send the new value."
	}
	return text
}

func promptResultViewText(prompt PromptData, surface Surface) string {
	text := promptPresentationText(prompt)
	if surface == SurfaceTelegram {
		return text + "\n/close to close"
	}
	return text
}

func commandViewsForSurface(surface Surface, state MenuState) []CommandView {
	views := BuildCommandView(state)
	switch surface {
	case SurfaceTelegramBotCommands:
		return publicBotCommandViews(views)
	default:
		return sharedCommandMenuViews(views)
	}
}

func sharedCommandMenuViews(views []CommandView) []CommandView {
	byID := make(map[string]CommandView, len(views))
	order := make([]string, 0, len(views))
	for _, view := range views {
		if !view.Public || !view.Menu || !commandVisibleInSharedMenu(view.ID) {
			continue
		}
		byID[view.ID] = view
		order = append(order, view.ID)
	}

	primary := []CommandID{
		CommandSessions,
		CommandContext,
		CommandProvider,
		CommandPermissions,
	}
	out := make([]CommandView, 0, len(byID))
	used := make(map[string]bool, len(primary))
	for _, id := range primary {
		view, ok := byID[string(id)]
		if !ok {
			continue
		}
		out = append(out, commandMenuTitleOverride(view))
		used[view.ID] = true
	}
	for _, id := range order {
		if used[id] {
			continue
		}
		out = append(out, byID[id])
	}
	return out
}

func publicBotCommandViews(views []CommandView) []CommandView {
	out := make([]CommandView, 0, len(views))
	for _, view := range views {
		if !view.Public {
			continue
		}
		if !view.Menu && view.ID != string(CommandHelp) {
			continue
		}
		if strings.EqualFold(CommandName(view.Command), "tts") {
			continue
		}
		out = append(out, view)
	}
	return out
}

func commandVisibleInSharedMenu(id string) bool {
	switch id {
	case string(CommandNewSession), string(CommandMemory):
		return false
	default:
		return true
	}
}

func commandMenuTitleOverride(view CommandView) CommandView {
	switch view.ID {
	case string(CommandProvider):
		view.Title = "Providers"
	case string(CommandPermissions):
		view.Title = "Permissions"
	}
	return view
}

func commandResultViewItem(view CommandView) ResultViewItem {
	title := strings.TrimSpace(view.Title)
	return ResultViewItem{
		ID:       view.ID,
		Label:    title,
		Title:    title,
		Info:     strings.TrimSpace(view.Status),
		Command:  strings.TrimSpace(view.Command),
		Disabled: view.Disabled,
	}
}

func pagedPickerForView(picker PickerData, options PickerViewOptions) PickerPage {
	if options.PageSize <= 0 {
		return PickerPage{
			Items: append([]PickerItem(nil), picker.Items...),
			Page:  0,
			Pages: 1,
		}
	}
	return PaginatePicker(picker, options.Page, options.PageSize)
}

func pickerResultViewItems(picker PickerData, items []PickerItem, surface Surface) []ResultViewItem {
	out := make([]ResultViewItem, 0, len(items))
	for _, item := range pickerViewItemsWithSeparators(picker, items) {
		presented := presentPickerItem(picker, item.Item)
		viewItem := pickerResultViewItem(presented, surface)
		viewItem.SeparatorBefore = item.SeparatorBefore
		out = append(out, viewItem)
	}
	return out
}

func pickerViewItemsWithSeparators(picker PickerData, items []PickerItem) []PickerViewItem {
	filtered := picker
	filtered.Items = append([]PickerItem(nil), items...)
	return PickerViewItems(filtered)
}

func pickerResultViewItem(item pickerPresentationItem, surface Surface) ResultViewItem {
	title := strings.TrimSpace(item.Title)
	label := pickerViewItemLabel(item, surface)
	return ResultViewItem{
		ID:       strings.TrimSpace(item.Item.ID),
		Label:    label,
		Title:    title,
		Info:     strings.TrimSpace(item.Info),
		Search:   strings.TrimSpace(item.Search),
		Command:  strings.TrimSpace(item.Command),
		Selected: item.Selected,
		Focused:  item.Item.Focused,
		Disabled: item.Disabled,
		Role:     item.Item.Role,
	}
}

func pickerViewItemLabel(item pickerPresentationItem, surface Surface) string {
	label := strings.TrimSpace(item.Title)
	if surface == SurfaceTelegram {
		label = strings.TrimSpace(item.CompactLabel)
		if label == "" {
			label = strings.TrimSpace(item.Title)
		}
		if item.Selected {
			label = SelectedMarker + " " + strings.TrimSpace(strings.TrimPrefix(label, SelectedMarker+" "))
		}
		return label
	}
	if label == "" {
		label = strings.TrimSpace(item.Item.ID)
	}
	return label
}

func pickerResultViewFooter(picker PickerData, surface Surface) *ResultViewFooter {
	if footer, ok := PickerFooter(picker); ok {
		command := strings.TrimSpace(footer.Command)
		kind := FooterClose
		if picker.HasBack {
			kind = FooterBack
		}
		return &ResultViewFooter{
			Label:   pickerFooterLabel(footer.Label, kind),
			Command: command,
			Kind:    kind,
			Hidden:  picker.Select && surface == SurfaceTerminal && kind == FooterClose,
		}
	}
	if surface == SurfaceTelegram {
		return &ResultViewFooter{Label: "Close", Kind: FooterClose}
	}
	return nil
}

func pickerFooterLabel(label string, kind FooterKind) string {
	if kind == FooterBack {
		return "Back"
	}
	label = strings.TrimSpace(label)
	if label != "" {
		return label
	}
	return "Close"
}

func infoViewFooter(info InfoData, surface Surface) *ResultViewFooter {
	command := strings.TrimSpace(info.CloseCommand)
	if command != "" {
		return &ResultViewFooter{
			Label:   "Close",
			Command: command,
			Kind:    FooterClose,
			Hidden:  surface == SurfaceTerminal,
		}
	}
	if surface == SurfaceTelegram {
		return &ResultViewFooter{Label: "Close", Kind: FooterClose}
	}
	return nil
}

func textEditViewFooter(textEdit TextEditData) *ResultViewFooter {
	command := strings.TrimSpace(textEdit.CancelCommand)
	if command == "" {
		return nil
	}
	return &ResultViewFooter{Label: "Close", Command: command, Kind: FooterClose}
}

func normalizeSurface(surface Surface) Surface {
	switch surface {
	case SurfaceTerminal, SurfaceTelegram, SurfaceTelegramBotCommands:
		return surface
	default:
		return SurfaceTerminal
	}
}
