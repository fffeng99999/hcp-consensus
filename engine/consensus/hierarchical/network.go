package hierarchical

import "github.com/fffeng99999/hcap-consensus/engine/core"

// FilteredNetwork 包装底层网络，只允许向指定节点发送消息。
type FilteredNetwork struct {
	core.Network
	allowedNodes map[string]bool
	nodeID       string
}

// Send 仅在目标节点属于允许列表，或消息为广播语义时发送。
func (fn *FilteredNetwork) Send(msg *core.Message) error {
	if msg.To == "" || fn.allowedNodes[msg.To] {
		return fn.Network.Send(msg)
	}
	return nil
}

// Broadcast 只广播给允许列表中的节点，并排除当前节点自身。
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
