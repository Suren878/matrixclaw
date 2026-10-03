package api

import (
	"testing"

	"github.com/Suren878/matrixclaw/internal/modules"
)

func TestNonOwnersSeeOnlyWhetherAKeyIsSet(t *testing.T) {
	items := hideSecretPreviews([]modules.Item{
		{Key: "key", Kind: modules.ItemSecret, Display: "****L5Fy"},
		{Key: "unset", Kind: modules.ItemSecret, Display: secretNotSet},
		{Key: "url", Kind: modules.ItemText, Display: "http://localhost:8888"},
		{Key: "page", Kind: modules.ItemPage, Items: []modules.Item{{Key: "token", Kind: modules.ItemSecret, Display: "****3c1a"}}},
	})
	if items[0].Display != "Set" || items[1].Display != secretNotSet || items[2].Display != "http://localhost:8888" || items[3].Items[0].Display != "Set" {
		t.Fatalf("items = %+v", items)
	}
}
