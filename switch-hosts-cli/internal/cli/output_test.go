package cli

import (
	"bytes"
	"testing"
	"time"
)

func TestTimestampWriterPrefixesEachLine(t *testing.T) {
	var buf bytes.Buffer
	now := func() time.Time {
		return time.Date(2026, 5, 31, 9, 8, 7, 0, time.UTC)
	}

	writer := newTimestampWriter(&buf, now)

	if _, err := writer.Write([]byte("first line\nsecond line\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	want := "[2026-05-31 09:08:07] first line\n[2026-05-31 09:08:07] second line\n"
	if got := buf.String(); got != want {
		t.Fatalf("unexpected output:\nwant: %q\ngot:  %q", want, got)
	}
}

func TestTimestampWriterPrefixesContinuedLinesOnlyOnce(t *testing.T) {
	var buf bytes.Buffer
	now := func() time.Time {
		return time.Date(2026, 5, 31, 9, 8, 7, 0, time.UTC)
	}

	writer := newTimestampWriter(&buf, now)

	if _, err := writer.Write([]byte("partial")); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := writer.Write([]byte(" line\nnext\n")); err != nil {
		t.Fatalf("second write: %v", err)
	}

	want := "[2026-05-31 09:08:07] partial line\n[2026-05-31 09:08:07] next\n"
	if got := buf.String(); got != want {
		t.Fatalf("unexpected output:\nwant: %q\ngot:  %q", want, got)
	}
}
