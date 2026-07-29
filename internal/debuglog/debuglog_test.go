package debuglog

import "testing"

// reset clears all recorded entries between tests, since entries is
// package-level state shared across this file's test functions.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	entries = nil
}

func TestLogAppendsFormattedEntries(t *testing.T) {
	reset()
	Info("hello %d", 1)
	Info("world")

	got := Entries()
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].Text != "hello 1" || got[1].Text != "world" {
		t.Errorf("got %+v", got)
	}
}

func TestLeveledFuncsTagTheirLevel(t *testing.T) {
	reset()
	Debug("d")
	Info("i")
	Warn("w")
	Error("e")

	got := Entries()
	if len(got) != 4 {
		t.Fatalf("got %d entries, want 4", len(got))
	}
	want := []Level{LevelDebug, LevelInfo, LevelWarn, LevelError}
	for i, w := range want {
		if got[i].Level != w {
			t.Errorf("entry %d: got level %v, want %v", i, got[i].Level, w)
		}
	}
}

func TestLevelsAreOrderedBySeverity(t *testing.T) {
	if LevelDebug >= LevelInfo || LevelInfo >= LevelWarn || LevelWarn >= LevelError {
		t.Errorf("expected Debug < Info < Warn < Error, got %v %v %v %v", LevelDebug, LevelInfo, LevelWarn, LevelError)
	}
}

func TestLogCapsAtMaxEntries(t *testing.T) {
	reset()
	for i := 0; i < maxEntries+10; i++ {
		Info("entry %d", i)
	}

	got := Entries()
	if len(got) != maxEntries {
		t.Fatalf("got %d entries, want %d", len(got), maxEntries)
	}
	if got[0].Text != "entry 10" {
		t.Errorf("expected oldest surviving entry to be %q, got %q", "entry 10", got[0].Text)
	}
	if last := got[len(got)-1].Text; last != "entry 1009" {
		t.Errorf("expected newest entry to be %q, got %q", "entry 1009", last)
	}
}

func TestEntriesReturnsACopy(t *testing.T) {
	reset()
	Info("one")

	got := Entries()
	got[0].Text = "mutated"

	got2 := Entries()
	if got2[0].Text != "one" {
		t.Errorf("Entries should return a copy; internal state was mutated to %q", got2[0].Text)
	}
}
