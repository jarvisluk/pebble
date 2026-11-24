package desktop

import "strings"

// TargetSpec represents a parsed desktop target string:
//   - "local" (default local desktop)
//   - "virtual:<slot>" (dedicated local Linux virtual display slot)
//   - "<nodeId>" (remote node default desktop)
//   - "<nodeId>:virtual:<slot>" (dedicated remote Linux virtual display slot)
type TargetSpec struct {
	Raw         string
	NodeID      string
	VirtualSlot string
}

// ParseTarget normalizes and parses a desktop target identifier.
func ParseTarget(raw string) TargetSpec {
	t := strings.TrimSpace(raw)
	if t == "" || strings.EqualFold(t, "local") {
		return TargetSpec{Raw: "local"}
	}
	if len(t) > len("local:virtual:") && strings.EqualFold(t[:len("local:virtual:")], "local:virtual:") {
		slot := strings.TrimSpace(t[len("local:virtual:"):])
		if slot == "" {
			return TargetSpec{Raw: "local"}
		}
		return TargetSpec{
			Raw:         "virtual:" + slot,
			VirtualSlot: slot,
		}
	}
	if len(t) > len("virtual:") && strings.EqualFold(t[:len("virtual:")], "virtual:") {
		slot := strings.TrimSpace(t[len("virtual:"):])
		if slot == "" {
			return TargetSpec{Raw: "local"}
		}
		return TargetSpec{
			Raw:         "virtual:" + slot,
			VirtualSlot: slot,
		}
	}
	if idx := strings.Index(strings.ToLower(t), ":virtual:"); idx > 0 {
		nodeID := strings.TrimSpace(t[:idx])
		slot := strings.TrimSpace(t[idx+len(":virtual:"):])
		if nodeID == "" || strings.EqualFold(nodeID, "local") {
			if slot == "" {
				return TargetSpec{Raw: "local"}
			}
			return TargetSpec{Raw: "virtual:" + slot, VirtualSlot: slot}
		}
		if slot == "" {
			return TargetSpec{Raw: nodeID, NodeID: nodeID}
		}
		return TargetSpec{
			Raw:         nodeID + ":virtual:" + slot,
			NodeID:      nodeID,
			VirtualSlot: slot,
		}
	}
	return TargetSpec{
		Raw:    t,
		NodeID: t,
	}
}

func (t TargetSpec) IsLocal() bool {
	return t.NodeID == ""
}

func (t TargetSpec) IsVirtualSlot() bool {
	return t.VirtualSlot != ""
}

func (t TargetSpec) HostKey() string {
	if t.NodeID == "" {
		return "local"
	}
	return t.NodeID
}
