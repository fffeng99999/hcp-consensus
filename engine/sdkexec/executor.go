package sdkexec

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"cosmossdk.io/log"
	cosmosmath "cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	hcpapp "github.com/fffeng99999/hcp-consensus/app"
	"github.com/fffeng99999/hcp-consensus/engine/core"
)

// appOptions 实现 Cosmos SDK 的 AppOptions 接口
type appOptions map[string]any

func (o appOptions) Get(key string) any {
	return o[key]
}

// Executor 是 Cosmos SDK 执行适配器，将共识区块提交到 SDK 应用。
type Executor struct {
	mu sync.Mutex

	nodeID  string
	homeDir string
	chainID string

	app *hcpapp.App
	db  dbm.DB

	initialized     bool
	stateHash       string
	committedBlocks uint64
	committedTxs    uint64
	lastAppHash     []byte
}

// New 创建 SDK 执行器实例
func New(nodeID, homeDir, chainID string) (*Executor, error) {
	initSDKConfig()
	if chainID == "" {
		chainID = "hcp-engine-sdk"
	}
	if err := os.MkdirAll(filepath.Join(homeDir, "data"), 0755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(homeDir, "config"), 0755); err != nil {
		return nil, err
	}
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, filepath.Join(homeDir, "data"))
	if err != nil {
		return nil, err
	}
	options := appOptions{
		"home":                         homeDir,
		"chain-id":                     chainID,
		"consensus-engine":             "noop",
		"engine-sdk-minimal":           true,
		"engine-sdk-skip-init-genesis": true,
	}
	a := hcpapp.NewApp(log.NewNopLogger(), db, nil, false, options)
	if err := a.LoadVersion(0); err != nil {
		_ = db.Close()
		return nil, err
	}
	exec := &Executor{
		nodeID:  nodeID,
		homeDir: homeDir,
		chainID: chainID,
		app:     a,
		db:      db,
	}
	if err := exec.initChain(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return exec, nil
}

// ExecuteBlock 执行区块：调用 SDK 的 FinalizeBlock 和 Commit
func (e *Executor) ExecuteBlock(block *core.Block) error {
	if block == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.initialized {
		if err := e.initChain(); err != nil {
			return err
		}
	}

	txs := make([][]byte, 0, len(block.Txs))
	txIDs := make([]string, 0, len(block.Txs))
	for _, tx := range block.Txs {
		if tx == nil {
			continue
		}
		txs = append(txs, tx.Payload)
		txIDs = append(txIDs, tx.ID)
	}

	req := &abci.RequestFinalizeBlock{
		Txs:               txs,
		Hash:              []byte(block.Hash),
		Height:            int64(block.Height),
		Time:              block.Timestamp,
		ProposerAddress:   []byte(block.Proposer),
		DecidedLastCommit: abci.CommitInfo{},
	}
	finalizeResp, err := e.app.FinalizeBlock(req)
	if err != nil {
		_ = e.writeError(block, "finalize_block", err)
		return err
	}
	commitResp, err := e.app.Commit()
	if err != nil {
		_ = e.writeError(block, "commit", err)
		return err
	}

	e.committedBlocks = block.Height
	e.committedTxs += uint64(len(txs))
	e.lastAppHash = append(e.lastAppHash[:0], finalizeResp.AppHash...)
	e.stateHash = hex.EncodeToString(finalizeResp.AppHash)
	if e.stateHash == "" {
		e.stateHash = fmt.Sprintf("height-%d", block.Height)
	}

	return e.writeBlockData(block, txIDs, finalizeResp, commitResp)
}

// GetStateHash 获取当前状态哈希
func (e *Executor) GetStateHash() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stateHash
}

// Close 关闭数据库连接
func (e *Executor) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.db != nil {
		return e.db.Close()
	}
	return nil
}

// initChain 初始化链：设置创世状态并注入负载生成账户
func (e *Executor) initChain() error {
	if e.initialized {
		return nil
	}
	genesisState := hcpapp.ModuleBasics.DefaultGenesis(e.app.AppCodec())
	if err := e.injectLoadgenAccounts(genesisState); err != nil {
		return err
	}
	appState, err := json.Marshal(genesisState)
	if err != nil {
		return err
	}
	genesisDoc := map[string]any{
		"chain_id":     e.chainID,
		"genesis_time": time.Now().UTC().Format(time.RFC3339Nano),
		"app_state":    genesisState,
	}
	if data, err := json.MarshalIndent(genesisDoc, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(e.homeDir, "config", "genesis.json"), data, 0644)
	}
	_, err = e.app.InitChain(&abci.RequestInitChain{
		ChainId:       e.chainID,
		InitialHeight: 1,
		Time:          time.Now(),
		AppStateBytes: appState,
	})
	if err != nil {
		return err
	}
	if err := e.seedLoadgenAccounts(); err != nil {
		return err
	}
	e.initialized = true
	e.stateHash = hex.EncodeToString(e.app.LastCommitID().Hash)
	e.writeLatest(0, nil, 0, 0)
	return nil
}

// injectLoadgenAccounts 向创世状态注入负载生成器账户
func (e *Executor) injectLoadgenAccounts(genesisState map[string]json.RawMessage) error {
	addresses, err := readAccountFile(os.Getenv("HCP_ENGINE_SDK_ACCOUNT_FILE"))
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		return nil
	}
	denom := os.Getenv("HCP_ENGINE_SDK_DENOM")
	if denom == "" {
		denom = "uhcp"
	}
	balance := int64(1_000_000_000)
	if raw := os.Getenv("HCP_ENGINE_SDK_ACCOUNT_BALANCE"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			balance = parsed
		}
	}

	genAccounts := make([]authtypes.GenesisAccount, 0, len(addresses))
	balances := make([]banktypes.Balance, 0, len(addresses))
	supply := sdk.NewCoins()
	for i, addrStr := range addresses {
		addr, err := sdk.AccAddressFromBech32(addrStr)
		if err != nil {
			return fmt.Errorf("invalid SDK account address %q: %w", addrStr, err)
		}
		baseAccount := authtypes.NewBaseAccountWithAddress(addr)
		baseAccount.AccountNumber = uint64(i)
		genAccounts = append(genAccounts, baseAccount)

		coins := sdk.NewCoins(sdk.NewCoin(denom, cosmosmath.NewInt(balance)))
		balances = append(balances, banktypes.Balance{
			Address: addrStr,
			Coins:   coins,
		})
		supply = supply.Add(coins...)
	}

	packedAccounts, err := authtypes.PackAccounts(genAccounts)
	if err != nil {
		return err
	}
	authGenesis := authtypes.DefaultGenesisState()
	authGenesis.Accounts = packedAccounts
	genesisState[authtypes.ModuleName] = e.app.AppCodec().MustMarshalJSON(authGenesis)

	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = balances
	bankGenesis.Supply = supply
	genesisState[banktypes.ModuleName] = e.app.AppCodec().MustMarshalJSON(bankGenesis)

	accountDoc := map[string]any{
		"account_count": len(addresses),
		"denom":         denom,
		"balance":       balance,
	}
	_ = writeJSON(filepath.Join(e.homeDir, "config", "funded_accounts.json"), accountDoc)
	return nil
}

// seedLoadgenAccounts 在链初始化后播种账户余额
func (e *Executor) seedLoadgenAccounts() error {
	addresses, err := readAccountFile(os.Getenv("HCP_ENGINE_SDK_ACCOUNT_FILE"))
	if err != nil {
		return err
	}
	if len(addresses) == 0 {
		return nil
	}
	denom := os.Getenv("HCP_ENGINE_SDK_DENOM")
	if denom == "" {
		denom = "uhcp"
	}
	balance := int64(1_000_000_000)
	if raw := os.Getenv("HCP_ENGINE_SDK_ACCOUNT_BALANCE"); raw != "" {
		if parsed, err := strconv.ParseInt(raw, 10, 64); err == nil && parsed > 0 {
			balance = parsed
		}
	}

	ctx := e.app.NewUncachedContext(false, cmtproto.Header{
		ChainID: e.chainID,
		Height:  0,
		Time:    time.Now(),
	})
	genAccounts := make([]authtypes.GenesisAccount, 0, len(addresses))
	balances := make([]banktypes.Balance, 0, len(addresses))
	supply := sdk.NewCoins()
	for i, addrStr := range addresses {
		addr, err := sdk.AccAddressFromBech32(addrStr)
		if err != nil {
			return fmt.Errorf("invalid SDK account address %q: %w", addrStr, err)
		}
		baseAccount := authtypes.NewBaseAccountWithAddress(addr)
		baseAccount.AccountNumber = uint64(i)
		genAccounts = append(genAccounts, baseAccount)

		coins := sdk.NewCoins(sdk.NewCoin(denom, cosmosmath.NewInt(balance)))
		balances = append(balances, banktypes.Balance{
			Address: addrStr,
			Coins:   coins,
		})
		supply = supply.Add(coins...)
	}
	authGenesis := authtypes.DefaultGenesisState()
	packedAccounts, err := authtypes.PackAccounts(genAccounts)
	if err != nil {
		return err
	}
	authGenesis.Accounts = packedAccounts
	e.app.AccountKeeper.InitGenesis(ctx, *authGenesis)

	bankGenesis := banktypes.DefaultGenesisState()
	bankGenesis.Balances = balances
	bankGenesis.Supply = supply
	e.app.BankKeeper.InitGenesis(ctx, bankGenesis)
	return nil
}

// accountFileRecord 账户文件记录结构
type accountFileRecord struct {
	Address string `json:"address"`
}

// readAccountFile 读取账户文件
func readAccountFile(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var addresses []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var record accountFileRecord
		if err := json.Unmarshal(line, &record); err != nil {
			return nil, err
		}
		if record.Address != "" {
			addresses = append(addresses, record.Address)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return addresses, nil
}

// writeBlockData 写入区块数据和交易结果
func (e *Executor) writeBlockData(block *core.Block, txIDs []string, finalizeResp *abci.ResponseFinalizeBlock, commitResp *abci.ResponseCommit) error {
	if err := os.MkdirAll(filepath.Join(e.homeDir, "blocks"), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(e.homeDir, "tx_results"), 0755); err != nil {
		return err
	}
	blockDoc := map[string]any{
		"node_id":       e.nodeID,
		"chain_id":      e.chainID,
		"height":        block.Height,
		"block_hash":    block.Hash,
		"prev_hash":     block.PrevHash,
		"proposer":      block.Proposer,
		"tx_count":      len(block.Txs),
		"tx_ids":        txIDs,
		"time":          block.Timestamp.UTC().Format(time.RFC3339Nano),
		"app_hash":      hex.EncodeToString(finalizeResp.AppHash),
		"retain_height": commitResp.RetainHeight,
	}
	if err := writeJSON(filepath.Join(e.homeDir, "blocks", fmt.Sprintf("block_%06d.json", block.Height)), blockDoc); err != nil {
		return err
	}
	results := make([]map[string]any, 0, len(finalizeResp.TxResults))
	for i, res := range finalizeResp.TxResults {
		txID := ""
		if i < len(txIDs) {
			txID = txIDs[i]
		}
		results = append(results, map[string]any{
			"tx_id":      txID,
			"code":       res.Code,
			"codespace":  res.Codespace,
			"log":        res.Log,
			"gas_wanted": res.GasWanted,
			"gas_used":   res.GasUsed,
		})
	}
	if err := writeJSON(filepath.Join(e.homeDir, "tx_results", fmt.Sprintf("height_%06d.json", block.Height)), map[string]any{
		"height":     block.Height,
		"tx_results": results,
	}); err != nil {
		return err
	}
	return e.writeLatest(block.Height, finalizeResp.AppHash, len(block.Txs), len(finalizeResp.TxResults))
}

// writeLatest 写入最新状态摘要
func (e *Executor) writeLatest(height uint64, appHash []byte, txCount int, resultCount int) error {
	stateDir := filepath.Join(e.homeDir, "state")
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}
	return writeJSON(filepath.Join(stateDir, "latest.json"), map[string]any{
		"node_id":           e.nodeID,
		"chain_id":          e.chainID,
		"height":            height,
		"app_hash":          hex.EncodeToString(appHash),
		"state_hash":        e.stateHash,
		"committed_blocks":  e.committedBlocks,
		"committed_txs":     e.committedTxs,
		"last_tx_count":     txCount,
		"last_result_count": resultCount,
		"store": map[string]any{
			"backend": "goleveldb",
			"path":    filepath.Join(e.homeDir, "data", "application.db"),
		},
	})
}

// writeError 写入错误信息到文件
func (e *Executor) writeError(block *core.Block, phase string, err error) error {
	return writeJSON(filepath.Join(e.homeDir, "last_error.json"), map[string]any{
		"node_id": e.nodeID,
		"height":  block.Height,
		"phase":   phase,
		"error":   err.Error(),
		"time":    time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// writeJSON 将数据以 JSON 格式写入文件
func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

var _ core.Executor = (*Executor)(nil)
var _ = cmtproto.Header{}
var _ = storetypes.StoreTypeIAVL

var configOnce sync.Once

// initSDKConfig 初始化 SDK 配置（Bech32 前缀等）
func initSDKConfig() {
	configOnce.Do(func() {
		defer func() { _ = recover() }()
		cfg := sdk.GetConfig()
		cfg.SetBech32PrefixForAccount("hcp", "hcppub")
		cfg.SetBech32PrefixForValidator("hcpvaloper", "hcpvaloperpub")
		cfg.SetBech32PrefixForConsensusNode("hcpvalcons", "hcpvalconspub")
		cfg.Seal()
	})
}
