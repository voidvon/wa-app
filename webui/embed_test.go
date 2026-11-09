//go:build dashboard

package dashboard

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedDashboard(t *testing.T) {
	files, err := Files()
	if err != nil {
		t.Fatal(err)
	}
	index, err := fs.ReadFile(files, "index.html")
	if err != nil || !strings.Contains(string(index), "/assets/") {
		t.Fatal("built Dashboard entry point is missing")
	}
	entries, err := fs.Glob(files, "assets/*.js")
	if err != nil || len(entries) == 0 {
		t.Fatal("built Dashboard JavaScript is missing")
	}
}
