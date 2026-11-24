package syncer

import (
	"encoding/json"
	"strings"
)

// VCOrder represents the partial order between two vector clocks.
type VCOrder int

const (
	VCEqual VCOrder = iota
	VCBefore
	VCAfter
	VCConcurrent
)

// VectorClock maps persistent host IDs to monotonic logical counters.
type VectorClock map[string]uint64

// ParseVectorClock decodes a JSON string into a VectorClock.
func ParseVectorClock(raw string) VectorClock {
	vc := make(VectorClock)
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return vc
	}
	_ = json.Unmarshal([]byte(raw), &vc)
	return vc
}

// MarshalJSONString encodes the vector clock as compact JSON.
func (vc VectorClock) MarshalJSONString() string {
	if len(vc) == 0 {
		return "{}"
	}
	b, err := json.Marshal(vc)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// Clone returns a deep copy of vc.
func (vc VectorClock) Clone() VectorClock {
	out := make(VectorClock, len(vc))
	for k, v := range vc {
		if v > 0 {
			out[k] = v
		}
	}
	return out
}

// Increment advances the counter for hostID by 1 and returns the same clock.
func (vc VectorClock) Increment(hostID string) VectorClock {
	if hostID == "" {
		hostID = "local"
	}
	vc[hostID] = vc[hostID] + 1
	return vc
}

// Compare computes the causal ordering of vc relative to other.
func (vc VectorClock) Compare(other VectorClock) VCOrder {
	var less, greater bool
	for k, v := range vc {
		if v == 0 {
			continue
		}
		ov := other[k]
		if v < ov {
			less = true
		} else if v > ov {
			greater = true
		}
	}
	for k, ov := range other {
		if ov == 0 {
			continue
		}
		v := vc[k]
		if v < ov {
			less = true
		} else if v > ov {
			greater = true
		}
	}
	switch {
	case !less && !greater:
		return VCEqual
	case less && !greater:
		return VCBefore
	case !less && greater:
		return VCAfter
	default:
		return VCConcurrent
	}
}

// Merge returns the element-wise maximum of vc and other.
func (vc VectorClock) Merge(other VectorClock) VectorClock {
	out := vc.Clone()
	for k, ov := range other {
		if ov > out[k] {
			out[k] = ov
		}
	}
	return out
}

// MaxClock returns the maximum counter value across all entries in vc.
func (vc VectorClock) MaxClock() uint64 {
	var max uint64
	for _, v := range vc {
		if v > max {
			max = v
		}
	}
	return max
}
