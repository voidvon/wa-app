package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCommandsWithoutRuntimeConfiguration(t *testing.T) {
	t.Setenv("WA_SERVICE_TOKEN", "")
	t.Setenv("WA_APP_STORE", "")
	for _, command := range []string{"help", "version"} {
		var output bytes.Buffer
		if err := run(t.Context(), []string{command}, &output); err != nil || output.Len() == 0 {
			t.Fatalf("%s needs runtime configuration or produced no output", command)
		}
	}
	var output bytes.Buffer
	if err := run(t.Context(), []string{"unknown"}, &output); err == nil {
		t.Fatal("unknown command was accepted")
	}
	if err := run(t.Context(), []string{"serve", "unexpected"}, &output); err == nil {
		t.Fatal("extra arguments were ignored")
	}
	t.Setenv("WA_APP_STORE", "sqlite")
	t.Setenv("WA_APP_SQLITE_FILE", t.TempDir()+"/missing.sqlite3")
	if err := run(t.Context(), []string{"migrate"}, &output); err == nil || !strings.Contains(err.Error(), "existing SQLite") {
		t.Fatal("migration created a new database or required service credentials")
	}
}
