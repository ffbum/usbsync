//go:build windows

package ui

import "strings"

type ResultSummary struct {
	Status    string
	Warnings  []string
	Conflicts []string
	Failures  []string
}

func (r *ResultSummary) AddWarning(text string) {
	if text == "" {
		return
	}
	r.Warnings = append(r.Warnings, text)
}

func (r *ResultSummary) AddFailure(text string) {
	if text == "" {
		return
	}
	r.Failures = append(r.Failures, text)
}

func (r *ResultSummary) AddWorkspaceResetWarning(newWorkRoot string) {
	r.AddWarning("需要切换到新的工作目录：" + newWorkRoot)
}

func (r *ResultSummary) AddRetiredMachineNotice(displayName string) {
	r.AddWarning("已停止使用的机器不会再阻塞清理：" + displayName)
}

func (r ResultSummary) SummaryText() string {
	lines := []string{r.Status}

	if len(r.Warnings) > 0 {
		lines = append(lines, "警告："+strings.Join(r.Warnings, "；"))
	}
	if len(r.Conflicts) > 0 {
		lines = append(lines, "冲突："+strings.Join(r.Conflicts, "；"))
	}
	if len(r.Failures) > 0 {
		lines = append(lines, "失败："+strings.Join(r.Failures, "；"))
	}

	return strings.Join(lines, "\r\n")
}
