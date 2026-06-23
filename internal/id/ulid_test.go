package id

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewAtLengthAndAlphabet(t *testing.T) {
	got, err := NewAt(time.UnixMilli(0), bytes.NewReader(make([]byte, 10)))
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	if len(got) != encodedLen {
		t.Fatalf("length = %d, want %d", len(got), encodedLen)
	}
	for i, c := range got {
		if !strings.ContainsRune(crockford, c) {
			t.Errorf("char %d (%q) not in Crockford alphabet", i, c)
		}
	}
}

func TestNewAtIsDeterministic(t *testing.T) {
	entropy := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	a, err := NewAt(time.UnixMilli(123456789), bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	b, err := NewAt(time.UnixMilli(123456789), bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	if a != b {
		t.Errorf("same input produced different ULIDs: %q vs %q", a, b)
	}
}

func TestULIDsSortByTime(t *testing.T) {
	entropy := make([]byte, 10) // identical entropy isolates the timestamp.
	earlier, err := NewAt(time.UnixMilli(1000), bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	later, err := NewAt(time.UnixMilli(2000), bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	if !(earlier < later) {
		t.Errorf("expected %q < %q (earlier should sort first)", earlier, later)
	}
}

func TestNewAtEntropyError(t *testing.T) {
	if _, err := NewAt(time.UnixMilli(0), bytes.NewReader([]byte{1, 2})); err == nil {
		t.Error("expected error from short entropy source, got nil")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestNewAtPropagatesReaderError(t *testing.T) {
	if _, err := NewAt(time.UnixMilli(0), errReader{}); err == nil {
		t.Error("expected propagated reader error, got nil")
	}
}

func TestNewIsUnique(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for range n {
		got := New()
		if len(got) != encodedLen {
			t.Fatalf("New() length = %d", len(got))
		}
		if _, dup := seen[got]; dup {
			t.Fatalf("duplicate ULID generated: %q", got)
		}
		seen[got] = struct{}{}
	}
}
