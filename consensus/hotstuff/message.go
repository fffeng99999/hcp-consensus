package hotstuff

// MessageType represents the type of HotStuff message
type MessageType int

const (
	MessageTypeNewView MessageType = iota
	MessageTypePrepare
	MessageTypePreCommit
	MessageTypeCommit
	MessageTypeDecide
)

// ConsensusMessage represents a generic HotStuff message
type ConsensusMessage struct {
	Type           MessageType
	View           uint64
	SequenceNumber uint64
	Digest         string             // Hash of the block
	NodeID         string             // Sender ID
	Signature      []byte             // Signature of the sender
	Data           []byte             // Payload (e.g. block data)
	Justification  *QuorumCertificate // QC for the previous view
}

// QuorumCertificate (QC) represents a collection of votes
type QuorumCertificate struct {
	View       uint64
	NodeID     string // Leader ID who assembled the QC
	BlockHash  string
	Signatures map[string][]byte // Map of NodeID -> Signature
}

// VoteMessage represents a vote from a replica
type VoteMessage struct {
	Type      MessageType // Phase this vote is for (Prepare, PreCommit, Commit)
	View      uint64
	BlockHash string
	NodeID    string
	Signature []byte
	PartialQC *QuorumCertificate // Optional: for chaining
}
