package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteRelativeFile(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	for range 2 {
		store, err := NewSQLiteStoreFile(t.Context(), "./data/accounts.sqlite3")
		if err != nil {
			t.Fatal("relative SQLite file could not be opened:", err)
		}
		store.Close()
	}
	info, err := os.Stat(filepath.Join(directory, "data", "accounts.sqlite3"))
	if err != nil || info.IsDir() || info.Mode().Perm() != 0600 {
		t.Fatal("relative SQLite file was not created with private permissions")
	}
}
