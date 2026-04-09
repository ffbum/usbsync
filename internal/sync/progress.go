package sync

import "strconv"

type Event struct {
	Phase      string
	Item       string
	Detail     string
	Status     string
	Done       int
	Total      int
	TotalKnown bool
}

func (e Event) TotalLabel() string {
	if !e.TotalKnown {
		return "?"
	}

	return strconv.Itoa(e.Total)
}

type ProgressModel struct {
	rows []Event
}

func NewProgressModel() *ProgressModel {
	return &ProgressModel{}
}

func (m *ProgressModel) Append(event Event) {
	m.rows = append(m.rows, event)
}

func (m *ProgressModel) Rows() []Event {
	rows := make([]Event, len(m.rows))
	copy(rows, m.rows)
	return rows
}
