package tpbft

// MessageType represents the type of PBFT message
type MessageType int

const (
	MessageTypePrePrepare MessageType = iota
	MessageTypePrepare
	MessageTypeCommit
	MessageTypeRequest
	MessageTypeReply
)

// ConsensusMessage represents a generic PBFT message
type ConsensusMessage struct {
	Type           MessageType
	View           uint64
	SequenceNumber uint64
	Digest         string // Hash of the request/block
	NodeID         string // Sender ID
	Signature      []byte // Signature of the sender
	Data           []byte // Payload (e.g. block data for PrePrepare)
}

// RequestMessage represents a client request
type RequestMessage struct {
	Operation string
	Timestamp int64
	ClientID  string
}

// ReplyMessage represents a reply to the client
type ReplyMessage struct {
	View      uint64
	Timestamp int64
	ClientID  string
	NodeID    string
	Result    []byte
}
