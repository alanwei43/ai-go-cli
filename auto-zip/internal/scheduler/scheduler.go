// Package scheduler parses six-field cron expressions and runs serial jobs.
package scheduler

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

func Parse(expression string, now time.Time) (cron.Schedule, string, error) {
	if expression == "" {
		expression = fmt.Sprintf("%d %d %d * * *", now.Second(), now.Minute(), now.Hour())
	}
	fields := strings.Fields(expression)
	if len(fields) != 6 {
		return nil, "", fmt.Errorf("cron 必须包含六个字段：秒 分 时 日 月 周")
	}
	for i, field := range fields {
		if i != 3 && i != 5 && strings.Contains(field, "?") {
			return nil, "", fmt.Errorf("cron 的 ? 只能用于日或周字段")
		}
	}
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	spec := strings.Join(fields, " ")
	schedule, err := parser.Parse(spec)
	if err != nil {
		return nil, "", fmt.Errorf("无效 cron 表达式: %w", err)
	}
	if schedule.Next(now).IsZero() {
		return nil, "", fmt.Errorf("cron 表达式没有可执行的日期")
	}
	return schedule, spec, nil
}

// Run waits for the first scheduled time, then executes one job at a time.
// Times missed during a job are skipped. Failures are reported and retried at
// the next scheduled time; cancellation is passed to the job by its caller.
func Run(ctx context.Context, schedule cron.Schedule, job func() error, errors io.Writer) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		next := schedule.Next(time.Now())
		if next.IsZero() {
			return fmt.Errorf("cron 表达式没有下一次执行时间")
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if ctx.Err() != nil {
			return nil
		}
		if err := job(); err != nil && ctx.Err() == nil {
			fmt.Fprintf(errors, "打包失败: %v\n", err)
		}
	}
}
