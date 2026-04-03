package common

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConsensusDirectoryStructure(t *testing.T) {
	consensusRoot := filepath.Join("..")
	entries, err := os.ReadDir(consensusRoot)
	if err != nil {
		t.Fatalf("read consensus root failed: %v", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "common" {
			continue
		}
		algoDir := filepath.Join(consensusRoot, name)
		requiredConsensus := filepath.Join(algoDir, "consensus.go")
		if _, err := os.Stat(requiredConsensus); err != nil {
			t.Fatalf("algorithm %s missing consensus.go", name)
		}

		nodePath := filepath.Join(algoDir, "node.go")
		msgPath := filepath.Join(algoDir, "message.go")
		trustPath := filepath.Join(algoDir, "trust_scorer.go")
		selectorPath := filepath.Join(algoDir, "validator_selector.go")

		existsNode := fileExists(nodePath)
		existsMsg := fileExists(msgPath)
		existsTrust := fileExists(trustPath)
		existsSelector := fileExists(selectorPath)

		hasAnyFullSetFile := existsNode || existsMsg || existsTrust || existsSelector
		hasAllFullSetFiles := existsNode && existsMsg && existsTrust && existsSelector
		if hasAnyFullSetFile && !hasAllFullSetFiles {
			t.Fatalf("algorithm %s must contain full set: node.go message.go trust_scorer.go validator_selector.go", name)
		}

		testsDir := filepath.Join(algoDir, "tests")
		if fileExists(testsDir) {
			info, err := os.Stat(testsDir)
			if err != nil {
				t.Fatalf("algorithm %s tests path stat failed: %v", name, err)
			}
			if !info.IsDir() {
				t.Fatalf("algorithm %s tests must be a directory", name)
			}
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
