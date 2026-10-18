package controlplane

import (
	"testing"
	"time"
)

func TestFormStoreKeepsTypedValuesUntilClosedOrExpired(t *testing.T) {
	now := time.Unix(0, 0)
	store := newFormStore(func() time.Time { return now })
	id := store.open(skillDraftState{Name: "Review"})

	if _, ok := formValue[providerForm](store, id); ok {
		t.Fatal("a skill draft was read as a provider form")
	}
	if !store.put(id, skillDraftState{Name: "Review", Description: "diffs"}) {
		t.Fatal("put on an open form failed")
	}
	now = now.Add(formLifetime - time.Second)
	if draft, ok := formValue[skillDraftState](store, id); !ok || draft.Description != "diffs" {
		t.Fatalf("draft = %+v, %v", draft, ok)
	}
	now = now.Add(formLifetime - time.Second)
	if _, ok := formValue[skillDraftState](store, id); !ok {
		t.Fatal("a form in use expired")
	}
	now = now.Add(formLifetime)
	if _, ok := formValue[skillDraftState](store, id); ok || store.put(id, skillDraftState{}) {
		t.Fatal("an unused form outlived its lifetime")
	}

	closed := store.open(skillDraftState{})
	store.close(closed)
	if _, ok := formValue[skillDraftState](store, closed); ok {
		t.Fatal("a closed form is still open")
	}
}

func TestFormStoreDropsTheOldestWhenFull(t *testing.T) {
	now := time.Unix(0, 0)
	store := newFormStore(func() time.Time { return now })
	first := store.open(skillDraftState{})
	for range formLimit {
		now = now.Add(time.Second)
		store.open(skillDraftState{})
	}
	if _, ok := formValue[skillDraftState](store, first); ok {
		t.Fatal("the oldest form was kept over the limit")
	}
	if len(store.entries) != formLimit {
		t.Fatalf("store holds %d forms", len(store.entries))
	}
}
