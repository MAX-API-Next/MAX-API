package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestModelDoesNotUseLegacyGormQueryOptionLocks(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `gorm:query_option`) {
			t.Fatalf("%s uses legacy gorm:query_option locking", file)
		}
	}
}
