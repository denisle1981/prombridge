package delta

import (
	"fmt"
	"math"
	"sort"

	"github.com/example/prometheus-udp-relay/internal/metriccodec"
)

type StartupMode string
type SendMode string

const (
	StartupFull    StartupMode = "full"
	StartupCurrent StartupMode = "current"
	SendDelta      SendMode    = "delta"
	SendSnapshot   SendMode    = "snapshot"
)

type Result struct {
	Definitions  []metriccodec.Definition
	Samples      []metriccodec.Sample
	Deleted      []uint32
	Full         bool
	BaselineOnly bool
	Observed     int
	Changed      int
	Unchanged    int
}

type Tracker struct {
	startup     StartupMode
	sendMode    SendMode
	initialized bool
	nextWireID  uint32
	wireIDs     map[[16]byte]uint32
	defs        map[[16]byte]metriccodec.Definition
	prev        map[[16]byte]metriccodec.Sample
	sent        map[[16]byte]bool
}

func New(startup StartupMode, sendMode SendMode) (*Tracker, error) {
	if startup != StartupFull && startup != StartupCurrent {
		return nil, fmt.Errorf("invalid startup mode %q: use full or current", startup)
	}
	if sendMode != SendDelta && sendMode != SendSnapshot {
		return nil, fmt.Errorf("invalid send mode %q: use delta or snapshot", sendMode)
	}
	return &Tracker{startup: startup, sendMode: sendMode, nextWireID: 1, wireIDs: map[[16]byte]uint32{}, defs: map[[16]byte]metriccodec.Definition{}, prev: map[[16]byte]metriccodec.Sample{}, sent: map[[16]byte]bool{}}, nil
}

func (t *Tracker) Process(defs []metriccodec.Definition, samples []metriccodec.Sample, forceFull bool) Result {
	currentDefs := make(map[[16]byte]metriccodec.Definition, len(defs))
	currentSamples := make(map[[16]byte]metriccodec.Sample, len(samples))
	for _, d0 := range defs {
		d := d0
		wid, ok := t.wireIDs[d.ID]
		if !ok {
			wid = t.nextWireID
			t.nextWireID++
			t.wireIDs[d.ID] = wid
		}
		d.WireID = wid
		currentDefs[d.ID] = d
		t.defs[d.ID] = d
	}
	for _, s0 := range samples {
		s := s0
		if wid, ok := t.wireIDs[s.ID]; ok {
			s.WireID = wid
			currentSamples[s.ID] = s
		}
	}

	res := Result{Observed: len(currentSamples)}
	if !t.initialized {
		t.initialized = true
		t.prev = currentSamples
		if t.startup == StartupCurrent {
			res.BaselineOnly = true
			return res
		}
		res.Full = true
		res.Definitions = sortedDefinitions(currentDefs, nil)
		res.Samples = sortedSamples(currentSamples, nil)
		for id := range currentSamples {
			t.sent[id] = true
		}
		res.Changed = len(res.Samples)
		return res
	}

	// Deletions are sent only for series the gateway has previously seen.
	for id := range t.prev {
		if _, ok := currentSamples[id]; !ok {
			if t.sent[id] {
				res.Deleted = append(res.Deleted, t.wireIDs[id])
				delete(t.sent, id)
			}
		}
	}
	sort.Slice(res.Deleted, func(i, j int) bool { return res.Deleted[i] < res.Deleted[j] })

	if forceFull {
		res.Full = true
		if t.startup == StartupFull {
			res.Definitions = sortedDefinitions(currentDefs, nil)
			res.Samples = sortedSamples(currentSamples, nil)
			for id := range currentSamples {
				t.sent[id] = true
			}
		} else {
			// In current mode, a reconciliation must not resurrect baseline-only
			// series that have never changed since the agent started.
			res.Definitions = sortedDefinitions(currentDefs, t.sent)
			res.Samples = sortedSamples(currentSamples, t.sent)
		}
		res.Changed = len(res.Samples)
		res.Unchanged = res.Observed - res.Changed
		t.prev = currentSamples
		return res
	}

	changedIDs := map[[16]byte]bool{}
	for id, s := range currentSamples {
		old, ok := t.prev[id]
		changed := !ok || math.Float64bits(old.Value) != math.Float64bits(s.Value)
		if t.sendMode == SendSnapshot {
			changed = true
		}
		if changed {
			changedIDs[id] = true
			res.Samples = append(res.Samples, s)
			if !t.sent[id] {
				if d, ok := currentDefs[id]; ok {
					res.Definitions = append(res.Definitions, d)
				}
				t.sent[id] = true
			}
		}
	}
	sort.Slice(res.Samples, func(i, j int) bool { return res.Samples[i].WireID < res.Samples[j].WireID })
	sort.Slice(res.Definitions, func(i, j int) bool { return res.Definitions[i].WireID < res.Definitions[j].WireID })
	res.Changed = len(changedIDs)
	res.Unchanged = res.Observed - res.Changed
	t.prev = currentSamples
	return res
}

func sortedDefinitions(src map[[16]byte]metriccodec.Definition, include map[[16]byte]bool) []metriccodec.Definition {
	out := make([]metriccodec.Definition, 0, len(src))
	for id, d := range src {
		if include == nil || include[id] {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WireID < out[j].WireID })
	return out
}
func sortedSamples(src map[[16]byte]metriccodec.Sample, include map[[16]byte]bool) []metriccodec.Sample {
	out := make([]metriccodec.Sample, 0, len(src))
	for id, s := range src {
		if include == nil || include[id] {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WireID < out[j].WireID })
	return out
}
