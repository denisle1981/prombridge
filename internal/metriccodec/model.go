package metriccodec

import "time"

type Label struct{ Key, Value string }
type Definition struct {
	ID     [16]byte
	WireID uint32
	Name   string
	Labels []Label
}
type Sample struct {
	ID          [16]byte // populated by the sender parser; receiver may leave it empty
	WireID      uint32
	Value       float64
	TimestampMS int64
}
type Batch struct {
	SenderID    [16]byte
	Sequence    uint64
	CreatedAt   time.Time
	Full        bool
	Definitions []Definition
	Samples     []Sample
	Deleted     []uint32
}
