package hierarchical

// TrustScorer 复用命名以对齐 tpbft 目录结构，这里承载基础参数权重。
type TrustScorer struct {
	baseLatencyMs float64
	innerWeight   float64
	outerWeight   float64
	messageBytes  int
}

// NewTrustScorer 创建参数评分器。
func NewTrustScorer(cfg Config) *TrustScorer {
	return &TrustScorer{
		baseLatencyMs: cfg.BaseLatencyMs,
		innerWeight:   cfg.PhaseWeightInner,
		outerWeight:   cfg.PhaseWeightOuter,
		messageBytes:  cfg.MessageBytes,
	}
}

// BaseLatencyMs 返回基础链路时延。
func (s *TrustScorer) BaseLatencyMs() float64 {
	return s.baseLatencyMs
}

// InnerWeight 返回组内阶段权重。
func (s *TrustScorer) InnerWeight() float64 {
	return s.innerWeight
}

// OuterWeight 返回组间阶段权重。
func (s *TrustScorer) OuterWeight() float64 {
	return s.outerWeight
}

// MessageBytes 返回单条消息大小。
func (s *TrustScorer) MessageBytes() int {
	return s.messageBytes
}

// normalizeConfig 规范化分层配置。
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
	return cfg
}
