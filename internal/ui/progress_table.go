//go:build windows

package ui

import (
	"strconv"

	"github.com/lxn/walk"
	progress "usbsync/internal/sync"
)

type ProgressTableModel struct {
	walk.TableModelBase
	rows []progress.Event
}

func NewProgressTableModel() *ProgressTableModel {
	return &ProgressTableModel{}
}

func (m *ProgressTableModel) Reset(rows []progress.Event) {
	m.rows = append([]progress.Event(nil), rows...)
	m.PublishRowsReset()
}

func (m *ProgressTableModel) Append(event progress.Event) {
	m.rows = append(m.rows, event)
	m.PublishRowsReset()
}

func (m *ProgressTableModel) RowCount() int {
	return len(m.rows)
}

func (m *ProgressTableModel) Value(row, col int) any {
	ev := m.rows[row]

	switch col {
	case 0:
		return phaseLabel(ev.Phase)
	case 1:
		return ev.Item
	case 2:
		return strconv.Itoa(ev.Done)
	case 3:
		return ev.TotalLabel()
	case 4:
		return ev.Status
	case 5:
		return ev.Detail
	default:
		return ""
	}
}

func phaseLabel(phase string) string {
	switch phase {
	case "scan":
		return "扫描"
	case "initialize":
		return "初始化"
	case "apply":
		return "写回本地"
	case "conflict":
		return "冲突"
	case "commit":
		return "写入 U 盘"
	case "state":
		return "更新状态"
	default:
		return phase
	}
}
