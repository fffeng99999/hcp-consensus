package hierarchical

import "github.com/fffeng99999/hcp-consensus/engine/core"

// FilteredNetwork wraps a Network and only allows sending to specific nodes
type FilteredNetwork struct {
	core.Network
	allowedNodes map[string]bool
	nodeID       string
}

// Send only sends if the target is in allowedNodes or broadcast (empty To)
func (fn *FilteredNetwork) Send(msg *core.Message) error {
	if msg.To == "" || fn.allowedNodes[msg.To] {
		return fn.Network.Send(msg)
	}
	return nil
}

// Broadcast only sends to allowed nodes (excluding self)
func (fn *FilteredNetwork) Broadcast(msg *core.Message) error {
	for node := range fn.allowedNodes {
		if node == fn.nodeID {
			continue
		}
		m := *msg
		m.To = node
		fn.Network.Send(&m)
	}
	return nil
}
