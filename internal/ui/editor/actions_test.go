package editor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestEveryPopupActionRuns: the action popup offers Scope's InPopup
// actions and runs them through ExecuteAction, so each one must be
// something runAction actually handles — an action added to the popup
// without a case would otherwise silently do nothing.
func TestEveryPopupActionRuns(t *testing.T) {
	for _, a := range Scope.Actions {
		if !a.InPopup {
			continue
		}
		t.Run(a.ID, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "f.txt")
			if err := os.WriteFile(path, []byte("one two\nthree\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			v := NewView()
			v.Open(path)
			if !v.ExecuteAction(a.ID) {
				t.Errorf("ExecuteAction(%q) = false, want it handled", a.ID)
			}
		})
	}
}
