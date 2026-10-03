package updater

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckComparesBuildVersionWithLatestRelease(t *testing.T) {
	cases := []struct {
		current string
		latest  string
		want    bool
	}{
		{current: "0.1.19 (abc1234)", latest: "v0.1.20", want: true},
		{current: "0.1.19", latest: "v0.1.19", want: false},
		{current: "0.1.10", latest: "v0.1.9", want: false},
		{current: "dev", latest: "v9.9.9", want: false},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, `{"tag_name":"`+tc.latest+`","html_url":"https://example.test/release"}`)
		}))
		update, ok, err := Checker{BaseURL: server.URL}.Check(context.Background(), tc.current)
		server.Close()
		if err != nil {
			t.Fatalf("Check(%q): %v", tc.current, err)
		}
		if ok != tc.want {
			t.Fatalf("Check(%q) against %s = %v, want %v", tc.current, tc.latest, ok, tc.want)
		}
		if ok && (update.Current != "v0.1.19" || update.Latest != tc.latest) {
			t.Fatalf("update = %+v", update)
		}
	}
}
