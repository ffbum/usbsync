package sync

import "testing"

func TestProgressEventAllowsUnknownTotals(t *testing.T) {
	ev := Event{Phase: "scan", Done: 3, TotalKnown: false}
	if ev.Total != 0 {
		t.Fatalf("expected zero total when unknown, got %d", ev.Total)
	}
	if got := ev.TotalLabel(); got != "?" {
		t.Fatalf("unexpected total label: %s", got)
	}
}

func TestProgressModelAppendsEventsInOrder(t *testing.T) {
	model := NewProgressModel()
	model.Append(Event{Phase: "scan", Done: 1, Total: 4, TotalKnown: true})
	model.Append(Event{Phase: "pull", Done: 2, TotalKnown: false})

	rows := model.Rows()
	if len(rows) != 2 {
		t.Fatalf("unexpected row count: %d", len(rows))
	}
	if rows[0].Phase != "scan" || rows[1].Phase != "pull" {
		t.Fatalf("unexpected row order: %#v", rows)
	}
}
