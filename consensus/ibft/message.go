package ibft

type MessageType uint8

const (
	MessageTypePrePrepare MessageType = iota
	MessageTypePrepare
	MessageTypeCommit
	MessageTypeRoundChange
)

type Message struct {
	Type          MessageType
	From          string
	Height        int64
	Round         uint64
	Value         string
	PreparedRound uint64
	PreparedValue string
}

