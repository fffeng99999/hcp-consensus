package hierarchical_tpbft

import "strings"

// TrustScorer 复用命名以对齐 tpbft 目录结构，这里承载签名与阶段权重参数。
type TrustScorer struct {
	cfg Config
}

// NewTrustScorer 创建参数评分器。
func NewTrustScorer(cfg Config) *TrustScorer {
	return &TrustScorer{cfg: cfg}
}

// Config 返回内部标准化参数。
func (s *TrustScorer) Config() Config {
	return s.cfg
}

// normalizeConfig 对分层 TPBFT 参数做规范化。
func normalizeConfig(cfg Config) Config {
	if cfg.NodeCount <= 0 {
		cfg.NodeCount = 32
	}
	if cfg.GroupCount <= 0 && cfg.GroupSize > 0 {
		cfg.GroupCount = maxInt(1, cfg.NodeCount/cfg.GroupSize)
	}
	if cfg.GroupSize <= 0 && cfg.GroupCount > 0 {
		cfg.GroupSize = maxInt(1, cfg.NodeCount/cfg.GroupCount)
	}
	if cfg.GroupCount <= 0 {
		cfg.GroupCount = cfg.NodeCount
	}
	if cfg.GroupSize <= 0 {
		cfg.GroupSize = 1
	}
	if cfg.MessageBytes <= 0 {
		cfg.MessageBytes = 256
	}
	if cfg.BaseLatencyMs <= 0 {
		cfg.BaseLatencyMs = 1
	}
	if cfg.PhaseWeightInner <= 0 {
		cfg.PhaseWeightInner = 1
	}
	if cfg.PhaseWeightOuter <= 0 {
		cfg.PhaseWeightOuter = 1
	}
	if cfg.SigAlgorithm == "" {
		cfg.SigAlgorithm = "bls"
	}
	cfg.SigAlgorithm = strings.ToLower(cfg.SigAlgorithm)
	defaultGen, defaultVerify, defaultAgg := defaultSigProfile(cfg.SigAlgorithm)
	if cfg.SigGenMs <= 0 {
		cfg.SigGenMs = defaultGen
	}
	if cfg.SigVerifyMs <= 0 {
		cfg.SigVerifyMs = defaultVerify
	}
	if cfg.SigAggMs <= 0 {
		cfg.SigAggMs = defaultAgg
	}
	if cfg.OuterSigMode == "" {
		cfg.OuterSigMode = "threshold"
	}
	if cfg.OuterSigAlgorithm == "" {
		if strings.ToLower(cfg.OuterSigMode) == "ed25519" {
			cfg.OuterSigAlgorithm = "ed25519"
		} else {
			cfg.OuterSigAlgorithm = cfg.SigAlgorithm
		}
	}
	if cfg.BatchVerifyGain <= 0 {
		cfg.BatchVerifyGain = 1
	}
	if cfg.SigGenParallelism <= 0 {
		cfg.SigGenParallelism = 1
	}
	if cfg.SigVerifyParallelism <= 0 {
		cfg.SigVerifyParallelism = 1
	}
	if cfg.SigAggParallelism <= 0 {
		cfg.SigAggParallelism = 1
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 200
	}
	return cfg
}

// defaultSigProfile 返回不同签名算法的默认耗时画像。
func defaultSigProfile(algo string) (float64, float64, float64) {
	switch strings.ToLower(algo) {
	case "ed25519":
		return 0.35, 0.65, 1.6
	case "bls":
		fallthrough
	default:
		return 0.6, 1.2, 0.9
	}
}
