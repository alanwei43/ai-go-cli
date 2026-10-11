package scheduler

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	now := time.Date(2026, 3, 21, 9, 10, 11, 123, time.FixedZone("CST", 8*60*60))
	for _, test := range []struct {
		expression string
		next       time.Time
	}{
		{"", time.Date(2026, 3, 22, 9, 10, 11, 0, now.Location())},
		{"0 0 10 * * ?", time.Date(2026, 3, 21, 10, 0, 0, 0, now.Location())},
		{"0 */5 * * * *", time.Date(2026, 3, 21, 9, 15, 0, 0, now.Location())},
		{"0 0 2 * * 1", time.Date(2026, 3, 23, 2, 0, 0, 0, now.Location())},
		{"0 0,30 9-10 * * *", time.Date(2026, 3, 21, 9, 30, 0, 0, now.Location())},
	} {
		t.Run(test.expression, func(t *testing.T) {
			schedule, spec, err := Parse(test.expression, now)
			if err != nil {
				t.Fatal(err)
			}
			if test.expression == "" && spec != "11 10 9 * * *" {
				t.Fatalf("unexpected default: %s", spec)
			}
			if next := schedule.Next(now); !next.Equal(test.next) {
				t.Fatalf("next = %v, want %v", next, test.next)
			}
		})
	}
}

func TestRejectInvalidSchedules(t *testing.T) {
	for _, expression := range []string{" ", "* * * * *", "* * * * * * *", "@daily", "60 * * * * *", "0 0 0 * * 7", "? * * * * *", "0 ? * * * *", "0 0 0 31 2 *"} {
		if _, _, err := Parse(expression, time.Now()); err == nil {
			t.Errorf("accepted invalid schedule %q", expression)
		}
	}
}

type interval time.Duration

func (i interval) Next(now time.Time) time.Time { return now.Add(time.Duration(i)) }

func TestRunContinuesAfterFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var output bytes.Buffer
	calls := 0
	err := Run(ctx, interval(5*time.Millisecond), func() error {
		calls++
		if calls == 1 {
			return fmt.Errorf("temporary failure")
		}
		if calls == 3 {
			cancel()
		}
		return nil
	}, &output)
	if err != nil || calls != 3 || !strings.Contains(output.String(), "temporary failure") {
		t.Fatalf("err=%v calls=%d output=%s", err, calls, &output)
	}
}

func TestRunImmediatelyBeforeCron(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	now := time.Now()
	schedule, _, err := Parse(now.Add(time.Hour).Format("5 4 15")+" * * *", now)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = Run(ctx, schedule, func() error {
		calls++
		cancel()
		return nil
	}, io.Discard)
	if err != nil || calls != 1 {
		t.Fatalf("expected one startup job before cron: err=%v calls=%d", err, calls)
	}
}

func TestRunCancellationWhileWaiting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	calls := 0
	err := Run(ctx, interval(time.Hour), func() error {
		calls++
		return nil
	}, io.Discard)
	if err != nil || calls != 1 {
		t.Fatalf("expected only the startup job: err=%v calls=%d", err, calls)
	}
}

func TestRunCancellationDuringJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var output bytes.Buffer
	calls := 0
	err := Run(ctx, interval(time.Millisecond), func() error {
		calls++
		<-ctx.Done()
		return ctx.Err()
	}, &output)
	if err != nil || calls != 1 || output.Len() != 0 {
		t.Fatalf("err=%v calls=%d output=%s", err, calls, &output)
	}
}
