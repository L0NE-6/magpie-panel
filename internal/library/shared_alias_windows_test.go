//go:build windows

package library

import (
	"os"
	"path/filepath"
	"testing"
)

func linkSharedWithJunction(t *testing.T, h string) {
	t.Helper()
	shared := filepath.Join(h, ".agents/skills")
	if err := os.MkdirAll(filepath.Dir(shared), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(skillsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := junction(skillsDir(), shared); err != nil {
		t.Fatal(err)
	}
}

func TestSharedIsLibraryThroughJunction(t *testing.T) {
	t.Run("predicate", func(t *testing.T) {
		h := sandbox(t)
		linkSharedWithJunction(t, h)
		if !sharedIsLibrary() {
			t.Fatal("realDir did not resolve the junction to the library")
		}
	})

	t.Run("ticks", func(t *testing.T) {
		h := setUpAliasTicks(t)
		linkSharedWithJunction(t, h)
		checkAliasTicks(t, h)
	})

	t.Run("hand-over", func(t *testing.T) {
		h := setUpAliasHandOver(t)
		linkSharedWithJunction(t, h)
		checkAliasHandOver(t, h)
	})
}
