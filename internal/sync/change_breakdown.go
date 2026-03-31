package sync

import "fmt"

type ChangeBreakdown struct {
	Added    int
	Modified int
	Deleted  int
}

func CountLocalChangeBreakdown(changes []LocalChange) ChangeBreakdown {
	var summary ChangeBreakdown
	for _, change := range changes {
		summary.AddOp(change.Op)
	}
	return summary
}

func (c *ChangeBreakdown) AddOp(op string) {
	switch op {
	case "add", "mkdir":
		c.Added++
	case "modify":
		c.Modified++
	case "delete":
		c.Deleted++
	}
}

func (c ChangeBreakdown) Total() int {
	return c.Added + c.Modified + c.Deleted
}

func (c ChangeBreakdown) SummaryText() string {
	if c.Total() == 0 {
		return "没有发现新增、修改或删除"
	}
	return fmt.Sprintf("新增 %d 项，修改 %d 项，删除 %d 项", c.Added, c.Modified, c.Deleted)
}

func OperationLabel(op string) string {
	switch op {
	case "add", "mkdir":
		return "新增"
	case "modify":
		return "修改"
	case "delete":
		return "删除"
	default:
		return "完成"
	}
}
