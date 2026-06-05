package model

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

type SimulateRequest struct {
	Chain                   string                   `json:"chain" validate:"required"`
	BlockNumber             Uint256                  `json:"blockNumber"`
	ProjectPath             string                   `json:"projectPath,omitempty"`
	LabelOverrides          []LabelOverride          `json:"labelOverrides,omitempty" validate:"dive"`
	ERC20BalanceOverrides   []ERC20BalanceOverride   `json:"erc20BalanceOverrides,omitempty" validate:"dive"`
	ERC20ApprovalOverrides  []ERC20ApprovalOverride  `json:"erc20ApprovalOverrides,omitempty" validate:"dive"`
	ERC721ApprovalOverrides []ERC721ApprovalOverride `json:"erc721ApprovalOverrides,omitempty" validate:"dive"`
	StateOverride           *StateOverride           `json:"stateOverride,omitempty"`
	StateOverrideBytecode   string                   `json:"stateOverrideBytecode,omitempty" validate:"hex_bytes"`
	Compiler                *CompilerConfig          `json:"compiler,omitempty"`
	DecodeInternal          bool                     `json:"decodeInternal"`
	Sender                  string                   `json:"sender" validate:"required,eth_address"`
	Target                  string                   `json:"target" validate:"required,eth_address"`
	Data                    string                   `json:"data" validate:"hex_bytes"`
	Value                   Uint256                  `json:"value,omitempty"`
}

type TxRequest struct {
	Chain          string `json:"chain" validate:"required"`
	TxHash         string `json:"txHash" validate:"required,tx_hash"`
	DecodeInternal bool   `json:"decodeInternal"`
	Quick          bool   `json:"quick"`
}

type RecordKind string

const (
	RecordKindSimulation RecordKind = "simulation"
	RecordKindTx         RecordKind = "tx"
)

type LabelOverride struct {
	Account string `json:"account" validate:"required,eth_address"`
	Label   string `json:"label" validate:"required,notblank"`
}

type ERC20BalanceOverride struct {
	Token   string  `json:"token" validate:"required,eth_address"`
	Account string  `json:"account" validate:"required,eth_address"`
	Balance Uint256 `json:"balance" validate:"required"`
}

type ERC20ApprovalOverride struct {
	Token   string  `json:"token" validate:"required,eth_address"`
	Owner   string  `json:"owner" validate:"required,eth_address"`
	Spender string  `json:"spender" validate:"required,eth_address"`
	Amount  Uint256 `json:"amount" validate:"required"`
}

type ERC721ApprovalOverride struct {
	Token   string  `json:"token" validate:"required,eth_address"`
	Owner   string  `json:"owner" validate:"required,eth_address"`
	Spender string  `json:"spender" validate:"required,eth_address"`
	TokenID Uint256 `json:"tokenId" validate:"required"`
}

type StateOverride struct {
	Source       string `json:"source"`
	ContractName string `json:"contractName,omitempty"`
}

type CompilerConfig struct {
	Use               string  `json:"use,omitempty"`
	Offline           bool    `json:"offline,omitempty"`
	NoAutoDetect      bool    `json:"noAutoDetect,omitempty"`
	ViaIR             *bool   `json:"viaIR,omitempty"`
	UseLiteralContent bool    `json:"useLiteralContent,omitempty"`
	NoMetadata        bool    `json:"noMetadata,omitempty"`
	EVMVersion        string  `json:"evmVersion,omitempty"`
	Optimize          *bool   `json:"optimize,omitempty"`
	OptimizerRuns     *uint32 `json:"optimizerRuns,omitempty"`
	RevertStrings     string  `json:"revertStrings,omitempty"`
}

type HealthResponse struct {
	OK                bool `json:"ok"`
	Chains            int  `json:"chains"`
	MaxConcurrentRuns int  `json:"maxConcurrentRuns"`
}

type ChainsResponse struct {
	Chains       []string          `json:"chains"`
	ExplorerURLs map[string]string `json:"explorerUrls,omitempty"`
}

type ProjectsResponse struct {
	Projects []string `json:"projects"`
}

type BrowseProjectResponse struct {
	Path string `json:"path"`
}

type SimulationRecord struct {
	ID       string           `json:"id"`
	Kind     RecordKind       `json:"kind"`
	Request  map[string]any   `json:"request"`
	Response SimulateResponse `json:"response"`
}

type SimulateResponse struct {
	ID              string           `json:"id"`
	Success         bool             `json:"success"`
	ExitCode        int              `json:"exitCode"`
	DurationMillis  int64            `json:"durationMillis"`
	Trace           string           `json:"trace"`
	ERC20Transfers  []ERC20Transfer  `json:"erc20Transfers,omitempty"`
	BalanceAnalysis *BalanceAnalysis `json:"balanceAnalysis,omitempty"`
	Stdout          string           `json:"-"`
	Stderr          string           `json:"-"`
	Error           string           `json:"error,omitempty"`
	RunDir          string           `json:"-"`
	TestPath        string           `json:"-"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type ERC20Transfer struct {
	Token            string `json:"token"`
	From             string `json:"from"`
	To               string `json:"to"`
	Amount           string `json:"amount"`
	NormalizedAmount string `json:"normalizedAmount,omitempty"`
	Symbol           string `json:"symbol,omitempty"`
	LogoURL          string `json:"logoUrl,omitempty"`
}

type BalanceAnalysis struct {
	Changes    []TokenBalanceChange `json:"changes,omitempty"`
	UserTotals []UserUSDChange      `json:"userTotals,omitempty"`
}

type TokenBalanceChange struct {
	User      string   `json:"user"`
	Token     string   `json:"token"`
	Symbol    string   `json:"symbol,omitempty"`
	LogoURL   string   `json:"logoUrl,omitempty"`
	RawAmount string   `json:"rawAmount"`
	Amount    string   `json:"amount"`
	USDValue  *float64 `json:"usdValue,omitempty"`
}

type UserUSDChange struct {
	User     string  `json:"user"`
	USDValue float64 `json:"usdValue"`
}

type Uint256 string

func (u *Uint256) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	if raw == "null" {
		return nil
	}

	var value string
	if len(raw) >= 2 && raw[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	} else {
		value = raw
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	n := new(big.Int)
	if strings.HasPrefix(value, "0x") || strings.HasPrefix(value, "0X") {
		parsed, ok := n.SetString(value[2:], 16)
		if !ok {
			return fmt.Errorf("invalid uint256 hex value %q", value)
		}
		n = parsed
	} else {
		if strings.HasPrefix(value, "-") {
			return fmt.Errorf("uint256 cannot be negative: %q", value)
		}
		parsed, ok := n.SetString(value, 10)
		if !ok {
			return fmt.Errorf("invalid uint256 decimal value %q", value)
		}
		n = parsed
	}

	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
	if n.Sign() < 0 || n.Cmp(maxUint256) > 0 {
		return fmt.Errorf("uint256 out of range: %q", value)
	}

	*u = Uint256(n.String())
	return nil
}

func (u Uint256) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

func (u Uint256) String() string {
	if u == "" {
		return "0"
	}
	return string(u)
}
