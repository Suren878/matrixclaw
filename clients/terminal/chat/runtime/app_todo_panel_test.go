package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Suren878/matrixclaw/clients/terminal/chat/viewmodel"
	"github.com/Suren878/matrixclaw/internal/agent/todo"
	"github.com/Suren878/matrixclaw/internal/core"
	"github.com/Suren878/matrixclaw/internal/daemonclient"
)

func newTodoApp(t *testing.T, width int, list *todo.List) *appModel {
	t.Helper()
	m := newApp(context.Background(), nil)
	m.width, m.height = width, 40
	m.read = viewmodel.NewReadModel(core.ClientSnapshot{SessionID: "session_1", Todo: list})
	return m
}

func todoPanelText(m *appModel) string {
	return ansi.Strip(m.todoPanelView(m.layout().todoWidth, 30))
}

func TestTodoPanelShowsWhileTheListHasOpenItems(t *testing.T) {
	m := newTodoApp(t, 160, &todo.List{SessionID: "session_1", Items: []todo.Item{
		{Content: "Read the parser", Status: todo.Completed},
		{Content: "Fix the bug", ActiveForm: "Fixing the bug", Status: todo.InProgress},
		{Content: "Run the tests", Status: todo.Pending},
	}})

	if m.layout().todoWidth == 0 {
		t.Fatal("todo panel hidden while items are open")
	}
	text := todoPanelText(m)
	for _, want := range []string{"Todo", "1/3 done", "[✓] Read the parser", "[•] Fixing the bug", "[ ] Run the tests"} {
		if !strings.Contains(text, want) {
			t.Fatalf("panel lacks %q:\n%s", want, text)
		}
	}

	finished, _ := json.Marshal(todo.List{SessionID: "session_1", Items: []todo.Item{{Content: "Read the parser", Status: todo.Completed}}})
	if err := m.read.Apply(daemonclient.LiveEvent{Type: core.EventTodoUpdated, SessionID: "session_1", Payload: finished}); err != nil {
		t.Fatal(err)
	}
	if m.layout().todoWidth != 0 {
		t.Fatal("todo panel kept after every item was completed")
	}
}

func TestCtrlNShowsAndHidesTheTodoPanel(t *testing.T) {
	m := newTodoApp(t, 160, nil)

	m.toggleTodoPanel()
	if m.layout().todoWidth == 0 || !strings.Contains(todoPanelText(m), "No todo list yet") {
		t.Fatalf("panel after ctrl+n:\n%s", todoPanelText(m))
	}
	m.toggleTodoPanel()
	if m.layout().todoWidth != 0 {
		t.Fatal("todo panel still shown after a second ctrl+n")
	}

	narrow := newTodoApp(t, 100, nil)
	if cmd := narrow.toggleTodoPanel(); cmd == nil || narrow.todoPanel != todoPanelAuto {
		t.Fatal("a narrow terminal should show the list in a dialog instead of the panel")
	}
}

func TestShortTodoPanelKeepsTheItemInProgressInView(t *testing.T) {
	list := &todo.List{SessionID: "session_1"}
	for i := 1; i <= 12; i++ {
		status := todo.Completed
		if i == 9 {
			status = todo.InProgress
		} else if i > 9 {
			status = todo.Pending
		}
		list.Items = append(list.Items, todo.Item{Content: fmt.Sprintf("Step %d", i), ActiveForm: fmt.Sprintf("Doing step %d", i), Status: status})
	}
	m := newTodoApp(t, 160, list)

	text := ansi.Strip(m.todoPanelView(m.layout().todoWidth, 8))

	if !strings.Contains(text, "[•] Doing step 9") || !strings.Contains(text, "8/12 done") {
		t.Fatalf("short panel lost the item in progress:\n%s", text)
	}
}
