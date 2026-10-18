package store_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Suren878/matrixclaw/internal/agent/todo"
)

func TestSessionTodoIsReplacedAndSurvivesAReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "matrixclaw.db")
	st := openTestStore(t, path)
	createTestSession(t, st, "s1")
	if empty, err := st.GetSessionTodo(ctx, "s1"); err != nil || empty.SessionID != "s1" || len(empty.Items) != 0 {
		t.Fatalf("todo before any write = %+v err = %v", empty, err)
	}
	first := todo.List{SessionID: "s1", Items: []todo.Item{{Content: "a", Status: todo.Pending}}, ChainRunID: "run_1", UpdatedRunID: "run_1", UpdatedAt: testEpoch}
	second := todo.List{SessionID: "s1", Items: []todo.Item{{Content: "b", ActiveForm: "Doing b", Status: todo.InProgress}}, ChainRunID: "run_1", UpdatedRunID: "run_2", UpdatedAt: testEpoch}
	for _, list := range []todo.List{first, second} {
		if err := st.SaveSessionTodo(ctx, list); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openTestStore(t, path)
	defer func() { _ = reopened.Close() }()
	stored, err := reopened.GetSessionTodo(ctx, "s1")
	if err != nil || !reflect.DeepEqual(stored, second) {
		t.Fatalf("stored = %+v err = %v", stored, err)
	}
	if err := reopened.SaveSessionTodo(ctx, todo.List{SessionID: "s1", UpdatedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}
	if cleared, err := reopened.GetSessionTodo(ctx, "s1"); err != nil || len(cleared.Items) != 0 || cleared.UpdatedRunID != "" {
		t.Fatalf("cleared = %+v err = %v", cleared, err)
	}
}

func TestSessionTodoGoesWithItsSession(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	createTestSession(t, st, "s1")
	if err := st.SaveSessionTodo(ctx, todo.List{SessionID: "s1", Items: []todo.Item{{Content: "a", Status: todo.Pending}}, UpdatedAt: testEpoch}); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteSession(ctx, "s1"); err != nil {
		t.Fatal(err)
	}
	createTestSession(t, st, "s1")

	if list, err := st.GetSessionTodo(ctx, "s1"); err != nil || len(list.Items) != 0 {
		t.Fatalf("todo of a recreated session = %+v err = %v", list, err)
	}
}
