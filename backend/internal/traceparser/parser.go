package traceparser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
	"unicode"

	"foundry-tx-simulator/backend/internal/model"
)

type ParsedOutput struct {
	Trace          string
	ERC20Transfers []model.ERC20Transfer
}

type forgeOutput struct {
	Traces []forgeTraceEntry `json:"traces"`
}

type forgeTraceEntry struct {
	Kind string
	Body forgeTraceBody
}

type forgeTraceBody struct {
	Arena []forgeArenaNode `json:"arena"`
}

type forgeArenaNode struct {
	Parent   *int              `json:"parent"`
	Children []int             `json:"children"`
	Idx      int               `json:"idx"`
	Trace    forgeCallTrace    `json:"trace"`
	Logs     []json.RawMessage `json:"logs"`
}

type forgeCallTrace struct {
	Address string `json:"address"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`
	Success *bool  `json:"success"`
}

type forgeLog struct {
	Address string       `json:"address"`
	Emitter string       `json:"emitter"`
	Topics  []string     `json:"topics"`
	Data    string       `json:"data"`
	RawLog  *forgeRawLog `json:"raw_log"`
}

type forgeRawLog struct {
	Address string   `json:"address"`
	Emitter string   `json:"emitter"`
	Topics  []string `json:"topics"`
	Data    string   `json:"data"`
}

const erc20TransferTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

func ParseOutput(output string) (ParsedOutput, error) {
	raw := forgeJSONPayload(output)
	if len(raw) == 0 {
		return ParsedOutput{}, fmt.Errorf("forge json trace output is empty")
	}

	var payload forgeOutput
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ParsedOutput{}, fmt.Errorf("decode forge json trace: %w", err)
	}
	if len(payload.Traces) == 0 {
		return ParsedOutput{}, fmt.Errorf("forge json trace has no traces")
	}

	hasArena := false
	transfers := make([]model.ERC20Transfer, 0)
	for _, trace := range payload.Traces {
		hasArena = hasArena || len(trace.Body.Arena) > 0
		transfers = append(transfers, trace.erc20Transfers()...)
	}
	if !hasArena {
		return ParsedOutput{}, fmt.Errorf("forge json trace has no arena nodes")
	}

	return ParsedOutput{
		Trace:          indentJSON(raw),
		ERC20Transfers: transfers,
	}, nil
}

func forgeJSONPayload(output string) []byte {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}
	if strings.HasPrefix(output, "{") {
		return []byte(output)
	}
	start := strings.Index(output, "\n{")
	if start < 0 {
		return nil
	}
	return []byte(strings.TrimSpace(output[start+1:]))
}

func indentJSON(raw []byte) string {
	var output bytes.Buffer
	if err := json.Indent(&output, raw, "", "  "); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return output.String()
}

func (entry *forgeTraceEntry) UnmarshalJSON(data []byte) error {
	var tuple []json.RawMessage
	if err := json.Unmarshal(data, &tuple); err != nil {
		return err
	}
	if len(tuple) != 2 {
		return fmt.Errorf("trace entry must be a [kind, body] tuple")
	}
	if err := json.Unmarshal(tuple[0], &entry.Kind); err != nil {
		return err
	}
	return json.Unmarshal(tuple[1], &entry.Body)
}

func (entry forgeTraceEntry) erc20Transfers() []model.ERC20Transfer {
	arenaByID := make(map[int]forgeArenaNode, len(entry.Body.Arena))
	for _, item := range entry.Body.Arena {
		arenaByID[item.Idx] = item
	}
	root, ok := transactionRoot(entry.Body.Arena)
	if !ok {
		return nil
	}
	return collectERC20Transfers(root, arenaByID, "")
}

func transactionRoot(arena []forgeArenaNode) (forgeArenaNode, bool) {
	for index := len(arena) - 1; index >= 0; index-- {
		item := arena[index]
		if item.Parent != nil && *item.Parent == 0 {
			return item, true
		}
	}
	return forgeArenaNode{}, false
}

func collectERC20Transfers(item forgeArenaNode, arenaByID map[int]forgeArenaNode, tokenContext string) []model.ERC20Transfer {
	if isRevertedTrace(item.Trace) {
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(item.Trace.Kind), "DELEGATECALL") {
		tokenContext = item.Trace.Address
	}

	transfers := make([]model.ERC20Transfer, 0)
	for _, rawLog := range item.Logs {
		transfer, ok := erc20TransferFromLog(rawLog, tokenContext)
		if ok {
			transfers = append(transfers, transfer)
		}
	}
	for _, childID := range item.Children {
		child, ok := arenaByID[childID]
		if !ok {
			continue
		}
		transfers = append(transfers, collectERC20Transfers(child, arenaByID, tokenContext)...)
	}
	return transfers
}

func isRevertedTrace(trace forgeCallTrace) bool {
	if trace.Success != nil && !*trace.Success {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(trace.Status))
	return status == "revert" || status == "reverted"
}

func erc20TransferFromLog(raw json.RawMessage, tokenAddress string) (model.ERC20Transfer, bool) {
	var log forgeLog
	if err := json.Unmarshal(raw, &log); err != nil {
		return model.ERC20Transfer{}, false
	}
	if log.RawLog != nil {
		if log.Address == "" {
			log.Address = log.RawLog.Address
		}
		if log.Emitter == "" {
			log.Emitter = log.RawLog.Emitter
		}
		if len(log.Topics) == 0 {
			log.Topics = log.RawLog.Topics
		}
		if log.Data == "" {
			log.Data = log.RawLog.Data
		}
	}
	if log.Address == "" {
		log.Address = log.Emitter
	}
	if len(log.Topics) != 3 || normalizeHex(log.Topics[0]) != erc20TransferTopic {
		return model.ERC20Transfer{}, false
	}
	if len(strings.TrimPrefix(normalizeHex(log.Data), "0x")) != 64 {
		return model.ERC20Transfer{}, false
	}

	token := normalizeAddress(tokenAddress)
	if token == "" {
		token = normalizeAddress(log.Address)
	}
	from := topicAddress(log.Topics[1])
	to := topicAddress(log.Topics[2])
	amount := hexUintToDecimal(log.Data)
	if token == "" || from == "" || to == "" || amount == "" {
		return model.ERC20Transfer{}, false
	}

	return model.ERC20Transfer{
		Token:  token,
		From:   from,
		To:     to,
		Amount: amount,
	}, true
}

func normalizeHex(value string) string {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "0x") && !strings.HasPrefix(value, "0X") {
		return value
	}
	return "0x" + strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X"))
}

func normalizeAddress(value string) string {
	value = normalizeHex(value)
	if !isAddress(value) {
		return ""
	}
	return value
}

func topicAddress(topic string) string {
	topic = strings.TrimPrefix(normalizeHex(topic), "0x")
	if len(topic) < 40 {
		return ""
	}
	return "0x" + topic[len(topic)-40:]
}

func hexUintToDecimal(value string) string {
	n, ok := new(big.Int).SetString(strings.TrimPrefix(normalizeHex(value), "0x"), 16)
	if !ok {
		return ""
	}
	return n.String()
}

func isAddress(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 42 || !strings.HasPrefix(value, "0x") {
		return false
	}
	for _, r := range value[2:] {
		if !unicode.IsDigit(r) && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
