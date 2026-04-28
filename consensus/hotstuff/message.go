package hotstuff

// MessageType represents HotStuff message phases
type MessageType int

const (
	MessageTypeNewView MessageType = iota
	MessageTypePrepare
	MessageTypePreCommit
	MessageTypeCommit
	MessageTypeDecide
	MessageTypeTimeout
)

// Block represents a proposed block in the chain
type Block struct {
	Hash      string
	Height    uint64
	View      uint64
	Payload   []byte
	ParentQC  *QuorumCertificate // QC justifying this block's parent
	Proposer  string
	Timestamp int64
}

// QuorumCertificate aggregates 2f+1 votes via threshold signature
type QuorumCertificate struct {
	View       uint64
	BlockHash  string
	Signatures map[string][]byte // Individual signatures (simulated threshold)
	Aggregated []byte            // Threshold signature aggregate
	Signers    []string          // Nodes that signed
}

// NewQC creates a new quorum certificate
func NewQC(view uint64, blockHash string) *QuorumCertificate {
	return &QuorumCertificate{
		View:       view,
		BlockHash:  blockHash,
		Signatures: make(map[string][]byte),
		Signers:    make([]string, 0),
	}
}

// AddSignature adds a signature to the QC (simulates threshold aggregation)
func (qc *QuorumCertificate) AddSignature(nodeID string, sig []byte) {
	qc.Signatures[nodeID] = sig
	qc.Signers = append(qc.Signers, nodeID)
}

// IsQuorum checks if QC has 2f+1 signatures
func (qc *QuorumCertificate) IsQuorum(totalNodes, faultTolerance int) bool {
	return len(qc.Signers) >= 2*faultTolerance+1
}

// ConsensusMessage represents a HotStuff protocol message
type ConsensusMessage struct {
	Type           MessageType
	View           uint64
	SequenceNumber uint64
	Block          *Block              // Proposed block (for Prepare)
	NodeID         string              // Sender
	Signature      []byte              // Sender signature
	Justification  *QuorumCertificate  // QC proving previous phase completion
	TimeoutCert    *TimeoutCertificate // For view change (optional)
}

// TimeoutCertificate proves leader timeout with 2f+1 timeout messages
type TimeoutCertificate struct {
	View       uint64
	HighQC     *QuorumCertificate // Highest QC from signers
	Signatures map[string][]byte
	Signers    []string
}

// VoteMessage represents a replica's vote for a phase
type VoteMessage struct {
	Type      MessageType // Phase: Prepare, PreCommit, Commit
	View      uint64
	BlockHash string
	NodeID    string
	Signature []byte
	HighQC    *QuorumCertificate // Voter's highest QC (for safety)
}

// Phase represents HotStuff consensus phase
type Phase int

const (
	PhasePrepare Phase = iota
	PhasePreCommit
	PhaseCommit
	PhaseDecide
)
