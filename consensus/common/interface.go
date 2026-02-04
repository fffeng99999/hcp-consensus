package common

// Common consensus interfaces
type ConsensusEngine interface {
    Start() error
    Stop() error
}
