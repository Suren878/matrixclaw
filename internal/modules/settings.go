package modules

import (
	"context"
	"errors"
	"fmt"

	"github.com/Suren878/matrixclaw/internal/setup"
)

// Configurable is a module with a settings screen that clients render
// without knowing the module.
type Configurable interface {
	// Settings are the screen's rows; building them may probe (the user asked).
	Settings(ctx context.Context) []Item
	// Change sets the item at path to value (empty for an action). Slow work
	// such as a download runs here; the setup.json edit is returned.
	Change(ctx context.Context, path []string, value string) (Change, error)
}

// Change is what a settings change does to setup.json and which page the
// client shows next.
type Change struct {
	Config func(*setup.Config) error // nil when setup.json stays as it is
	// Reload applies the setup again though it did not change: what the
	// modules offer depends on something the change installed or removed.
	Reload  bool
	Open    []string // nil: the changed item's page
	Message string
}

var (
	ErrUnknownSetting = errors.New("unknown setting")
	ErrInvalidSetting = errors.New("invalid setting")
)

// ItemKind is how a client shows and edits an Item.
type ItemKind string

const (
	ItemToggle ItemKind = "toggle" // Value is "on" or "off"
	ItemChoice ItemKind = "choice" // Value is one of Options
	ItemText   ItemKind = "text"   // free text; an empty value clears it
	ItemSecret ItemKind = "secret" // never sent back; Display is a masked preview
	ItemAction ItemKind = "action" // changed with an empty value
	ItemPage   ItemKind = "page"   // a page of Items
	ItemInfo   ItemKind = "info"   // read-only Facts
)

// Item is one row of a settings screen. Key is one path segment
// ([a-z0-9_.-], unique among its siblings).
type Item struct {
	Key      string   `json:"key"`
	Kind     ItemKind `json:"kind"`
	Label    string   `json:"label"`
	Value    string   `json:"value,omitempty"`
	Display  string   `json:"display,omitempty"` // shown next to the label
	Hint     string   `json:"hint,omitempty"`    // prompt placeholder, or why it is disabled
	Confirm  string   `json:"confirm,omitempty"` // ask before running an action
	Danger   bool     `json:"danger,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
	Options  []Option `json:"options,omitempty"`
	Items    []Item   `json:"items,omitempty"`
	Facts    []Fact   `json:"facts,omitempty"`
}

// Option is one value of a choice; options with a Group are picked group
// first (a language, then a voice).
type Option struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Info     string `json:"info,omitempty"`
	Group    string `json:"group,omitempty"`
	Confirm  string `json:"confirm,omitempty"` // ask before choosing it
	Disabled bool   `json:"disabled,omitempty"`
}

// Settings is a module's screen: GET /v1/modules/{id}/settings.
type Settings struct {
	Status Status `json:"status"`
	Items  []Item `json:"items"`
}

// ChangeRequest is the body of POST /v1/modules/{id}/settings.
type ChangeRequest struct {
	Path  []string `json:"path"`
	Value string   `json:"value,omitempty"`
}

// ChangeResponse answers a change with the screen after it.
type ChangeResponse struct {
	Settings Settings `json:"settings"`
	Open     []string `json:"open,omitempty"`
	Message  string   `json:"message,omitempty"`
}

// Find is the item at path among items.
func Find(items []Item, path []string) (Item, bool) {
	for depth, key := range path {
		found := false
		for _, item := range items {
			if item.Key == key {
				if depth == len(path)-1 {
					return item, true
				}
				items, found = item.Items, true
				break
			}
		}
		if !found {
			return Item{}, false
		}
	}
	return Item{}, false
}

// Toggle reads a toggle's value.
func Toggle(value string) (bool, error) {
	switch value {
	case "on":
		return true, nil
	case "off":
		return false, nil
	default:
		return false, fmt.Errorf("%w: %q is not on or off", ErrInvalidSetting, value)
	}
}

// OnOff is a toggle's value.
func OnOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// Unknown is the error for a path no item has.
func Unknown(path []string) error {
	return fmt.Errorf("%w: %v", ErrUnknownSetting, path)
}
