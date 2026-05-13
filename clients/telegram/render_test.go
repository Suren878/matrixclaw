package telegram

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/Suren878/matrixclaw/internal/controlplane"
)

func TestRenderCommandResultUsesCompactTelegramPickers(t *testing.T) {
	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, "http://127.0.0.1:1")

	err := worker.renderCommandResult(context.Background(), chatTarget{
		chatID:      42,
		externalKey: "42",
	}, 0, controlplane.Result{
		Picker: &controlplane.PickerData{
			Kind:  controlplane.PickerProvider,
			Title: "Provider",
			Items: []controlplane.PickerItem{
				{ID: "openai", Title: "OpenAI"},
				{ID: "anthropic", Title: "Anthropic", Info: "Configured · claude-sonnet-4.5 · Active", Selected: true},
				controlplane.CloseItem(),
			},
		},
	})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	if len(api.sendMessageRequests) != 1 {
		t.Fatalf("sendMessageRequests len = %d, want 1", len(api.sendMessageRequests))
	}
	request := api.sendMessageRequests[0]
	if request.Text != "Provider" {
		t.Fatalf("request.Text = %q", request.Text)
	}
	if request.ReplyMarkup == nil || len(request.ReplyMarkup.InlineKeyboard) != 3 {
		t.Fatalf("reply markup = %+v, want 3 buttons", request.ReplyMarkup)
	}
	if got := cleanButtonText(request.ReplyMarkup.InlineKeyboard[0][0].Text); got != "🔌 OpenAI" {
		t.Fatalf("provider label = %q", got)
	}
	if got := cleanButtonText(request.ReplyMarkup.InlineKeyboard[1][0].Text); got != "✅ 🔌 Anthropic · Configured · claude-sonnet-4.5 · Active" {
		t.Fatalf("selected provider label = %q", got)
	}
	if got := cleanButtonText(request.ReplyMarkup.InlineKeyboard[2][0].Text); got != "✖️ Cancel" {
		t.Fatalf("cancel label = %q, want ✖️ Cancel", got)
	}
}

func TestRenderServerPickerUsesCompactEmojiRows(t *testing.T) {
	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, "http://127.0.0.1:1")

	err := worker.renderCommandResult(context.Background(), chatTarget{
		chatID:      42,
		externalKey: "42",
	}, 0, controlplane.Result{
		Picker: &controlplane.PickerData{
			Kind:  controlplane.PickerServer,
			Title: "Server",
			Items: []controlplane.PickerItem{
				{ID: "status", Title: "Status"},
				{ID: "restart", Title: "Restart Daemon"},
				controlplane.CloseItem(),
			},
		},
	})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	request := api.sendMessageRequests[0]
	if request.Text != "Server" {
		t.Fatalf("request.Text = %q", request.Text)
	}
	if request.ReplyMarkup == nil || len(request.ReplyMarkup.InlineKeyboard) != 3 {
		t.Fatalf("reply markup = %+v, want 3 rows", request.ReplyMarkup)
	}
	if got := cleanButtonText(request.ReplyMarkup.InlineKeyboard[0][0].Text); got != "📊 Status" {
		t.Fatalf("status label = %q", got)
	}
	if got := cleanButtonText(request.ReplyMarkup.InlineKeyboard[1][0].Text); got != "🔄 Restart Architect" {
		t.Fatalf("restart label = %q", got)
	}
	if got := cleanButtonText(request.ReplyMarkup.InlineKeyboard[2][0].Text); got != "✖️ Cancel" {
		t.Fatalf("cancel label = %q, want ✖️ Cancel", got)
	}
}

func TestRenderPickerUsesCommandCallbackForExplicitItemCommand(t *testing.T) {
	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, "http://127.0.0.1:1")

	err := worker.renderCommandResult(context.Background(), chatTarget{
		chatID:      42,
		externalKey: "42",
	}, 0, controlplane.Result{
		Picker: &controlplane.PickerData{
			Kind:  controlplane.PickerServer,
			Title: "Server",
			Items: []controlplane.PickerItem{
				{ID: "restart", Title: "Restart Daemon", Command: "/restart"},
			},
		},
	})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	request := api.sendMessageRequests[0]
	if request.ReplyMarkup == nil || len(request.ReplyMarkup.InlineKeyboard) != 1 || len(request.ReplyMarkup.InlineKeyboard[0]) != 1 {
		t.Fatalf("reply markup = %+v, want one restart button", request.ReplyMarkup)
	}
	got := request.ReplyMarkup.InlineKeyboard[0][0].CallbackData
	if got != commandCallbackData("/restart") {
		t.Fatalf("restart callback = %q, want command callback", got)
	}
}

func TestRenderPickerCompactsLongCommandCallbackData(t *testing.T) {
	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, "http://127.0.0.1:1")
	longCommand := "/provider edit set model custom-provider " + strings.Repeat("token", 20) + " claude-3-7-sonnet-20250219"
	if got := len([]byte(commandCallbackData(longCommand))); got <= maxCallbackDataBytes {
		t.Fatalf("test command callback len = %d, want > %d", got, maxCallbackDataBytes)
	}

	err := worker.renderCommandResult(context.Background(), chatTarget{
		chatID:      42,
		externalKey: "42",
	}, 0, controlplane.Result{
		Picker: &controlplane.PickerData{
			Kind:  controlplane.PickerProviderCustom,
			Title: "Model",
			Items: []controlplane.PickerItem{
				{ID: "claude", Title: "Claude", Command: longCommand},
			},
		},
	})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	callback := api.sendMessageRequests[0].ReplyMarkup.InlineKeyboard[0][0].CallbackData
	if got := len([]byte(callback)); got > maxCallbackDataBytes {
		t.Fatalf("callback_data len = %d, want <= %d", got, maxCallbackDataBytes)
	}
	if !strings.HasPrefix(callback, cbCallbackRef) {
		t.Fatalf("callback_data = %q, want compact ref prefix %q", callback, cbCallbackRef)
	}
	kind, command, ok := parsePickerCallbackData(worker.resolveCallbackData(callback))
	if !ok {
		t.Fatalf("parse compacted callback %q failed", callback)
	}
	if kind != callbackKindCommand || command != longCommand {
		t.Fatalf("resolved callback = (%q, %q), want command %q", kind, command, longCommand)
	}
}

func TestSendTextUsesHTMLParseMode(t *testing.T) {
	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, "http://127.0.0.1:1")

	if err := worker.sendText(context.Background(), chatTarget{chatID: 42}, "**bold**"); err != nil {
		t.Fatalf("sendText() error = %v", err)
	}
	if len(api.sendMessageRequests) != 1 {
		t.Fatalf("send requests = %d, want 1", len(api.sendMessageRequests))
	}
	if got := api.sendMessageRequests[0].ParseMode; got != defaultParseMode {
		t.Fatalf("ParseMode = %q, want %q", got, defaultParseMode)
	}
	if got := api.sendMessageRequests[0].Text; got != "<b>bold</b>" {
		t.Fatalf("Text = %q, want converted bold HTML", got)
	}
}

func TestSendTextFallsBackToPlainTextOnTelegramParseError(t *testing.T) {
	api := &fakeBotAPI{
		sendMessageErr: &APIError{
			ErrorCode:   400,
			Description: "Bad Request: can't parse entities",
		},
	}
	worker := newTestWorker(t, api, "http://127.0.0.1:1")

	if err := worker.sendText(context.Background(), chatTarget{chatID: 42}, "**broken"); err != nil {
		t.Fatalf("sendText() error = %v", err)
	}
	if len(api.sendMessageRequests) != 2 {
		t.Fatalf("send requests = %d, want 2", len(api.sendMessageRequests))
	}
	if got := api.sendMessageRequests[0].ParseMode; got != defaultParseMode {
		t.Fatalf("first ParseMode = %q, want %q", got, defaultParseMode)
	}
	if got := api.sendMessageRequests[1].ParseMode; got != "" {
		t.Fatalf("fallback ParseMode = %q, want empty", got)
	}
	if got := api.sendMessageRequests[1].Text; got != "**broken" {
		t.Fatalf("fallback text = %q, want original plain text", got)
	}
}

func TestRenderTelegramHTMLConvertsCommonAIMarkdown(t *testing.T) {
	input := strings.Join([]string{
		"**Жирный текст** и *курсив*",
		"",
		"`Инлайн код`",
		"",
		"```",
		"Блок кода",
		"```",
		"",
		"> Цитата",
		"",
		"- Пункт 1",
		"- Пункт 2",
		"",
		"[Ссылка](https://google.com)",
		"",
		"---",
	}, "\n")

	got := renderTelegramHTML(input)
	wantParts := []string{
		"<b>Жирный текст</b> и <i>курсив</i>",
		"<code>Инлайн код</code>",
		"<pre>Блок кода</pre>",
		"<blockquote>Цитата</blockquote>",
		"• Пункт 1",
		"• Пункт 2",
		`<a href="https://google.com">Ссылка</a>`,
		"────────",
	}
	for _, want := range wantParts {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered HTML missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderTelegramHTMLDoesNotFormatInsideInlineCode(t *testing.T) {
	got := renderTelegramHTML("`**not bold**` and **bold**")
	if strings.Contains(got, "<code><b>") {
		t.Fatalf("inline code was formatted as bold: %s", got)
	}
	if !strings.Contains(got, "<code>**not bold**</code>") {
		t.Fatalf("inline code was not preserved: %s", got)
	}
	if !strings.Contains(got, "<b>bold</b>") {
		t.Fatalf("normal bold was not formatted: %s", got)
	}
}

func TestRenderPickerPaginatesLargeLists(t *testing.T) {
	api := &fakeBotAPI{}
	worker := newTestWorker(t, api, "http://127.0.0.1:1")
	items := make([]controlplane.PickerItem, 0, 26)
	for i := 0; i < 25; i++ {
		items = append(items, controlplane.PickerItem{
			ID:    "model-" + strconv.Itoa(i),
			Title: "model-" + strconv.Itoa(i),
		})
	}
	items = append(items, controlplane.CloseItem())

	err := worker.renderCommandResult(context.Background(), chatTarget{
		chatID:      42,
		externalKey: "42",
	}, 0, controlplane.Result{
		Picker: &controlplane.PickerData{
			Kind:  controlplane.PickerProvider,
			Title: "Provider",
			Items: items,
		},
	})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	request := api.sendMessageRequests[0]
	if request.ReplyMarkup == nil {
		t.Fatal("expected reply markup")
	}
	if got := len(request.ReplyMarkup.InlineKeyboard); got != 22 {
		t.Fatalf("rows = %d, want 22", got)
	}
	nav := request.ReplyMarkup.InlineKeyboard[20]
	if len(nav) != 2 || nav[0].Text != "1/2" || nav[1].Text != "Next ›" {
		t.Fatalf("nav row = %#v", nav)
	}
	if got := cleanButtonText(request.ReplyMarkup.InlineKeyboard[21][0].Text); got != "✖️ Cancel" {
		t.Fatalf("cancel label = %q", got)
	}
}

func TestClipTelegramTextTruncatesLongMessage(t *testing.T) {
	text := strings.Repeat("a", defaultMessageLimit+50)
	clipped := clipTelegramText(text)
	if len([]rune(clipped)) != defaultMessageLimit {
		t.Fatalf("len(clipped) = %d, want %d", len([]rune(clipped)), defaultMessageLimit)
	}
	if !strings.HasSuffix(clipped, "…") {
		t.Fatalf("clipped = %q, want ellipsis suffix", clipped[len(clipped)-4:])
	}
}

func TestClipTelegramButtonTextNormalizesAndTruncates(t *testing.T) {
	text := "  🔌   " + strings.Repeat("a", defaultButtonTextLimit+10)
	clipped := clipTelegramButtonText(text)
	if len([]rune(clipped)) > defaultButtonTextLimit {
		t.Fatalf("len(clipped) = %d, want <= %d", len([]rune(clipped)), defaultButtonTextLimit)
	}
	if strings.Contains(clipped, "   ") {
		t.Fatalf("clipped should normalize whitespace: %q", clipped)
	}
	if !strings.HasSuffix(clipped, "…") {
		t.Fatalf("clipped = %q, want ellipsis suffix", clipped)
	}
}
