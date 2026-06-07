package scheduler

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// Scheduler 定时调度器
type Scheduler struct {
	cron *cron.Cron
}

// New 创建新的调度器
func New() *Scheduler {
	// 使用 WithSeconds 选项支持 6 字段 cron 表达式（秒 分 时 日 月 周）
	return &Scheduler{
		cron: cron.New(cron.WithSeconds()),
	}
}

// AddTask 添加定时任务
// cronExpr: cron 表达式，格式: "秒 分 时 日 月 周"
// task: 要执行的任务函数
func (s *Scheduler) AddTask(cronExpr string, task func()) error {
	_, err := s.cron.AddFunc(cronExpr, task)
	if err != nil {
		return fmt.Errorf("添加定时任务失败: %w", err)
	}
	return nil
}

// Start 启动调度器
func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	s.cron.Stop()
}

// GetDefaultCronExpr 获取默认的 cron 表达式
// 默认为每天的当前时间执行一次
func GetDefaultCronExpr() string {
	now := time.Now()
	return fmt.Sprintf("%d %d %d * * ?", now.Second(), now.Minute(), now.Hour())
}

// ValidateCronExpr 验证 cron 表达式是否有效
func ValidateCronExpr(expr string) error {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	if err != nil {
		return fmt.Errorf("无效的 cron 表达式: %w", err)
	}
	return nil
}
