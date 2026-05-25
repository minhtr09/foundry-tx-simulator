package solidity

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"foundry-tx-simulator/backend/internal/model"
)

var (
	addressPattern      = regexp.MustCompile(`^0x[0-9a-fA-F]{40}$`)
	bytesPattern        = regexp.MustCompile(`^0x([0-9a-fA-F]{2})*$`)
	contractNamePattern = regexp.MustCompile(`(?m)\bcontract\s+([A-Za-z_][A-Za-z0-9_]*)\b`)
	ansiPattern         = regexp.MustCompile(`\x1b\[[0-9;]*m`)
	hexValuePattern     = regexp.MustCompile(`0x[0-9a-fA-F]+`)
)

func ForgeRunArgs(req model.SimulateRequest, stateBytecode string) []string {
	stateBytecode, _ = NormalizeBytes("stateBytecode", stateBytecode)
	data, _ := NormalizeBytes("data", req.Data)
	return []string{
		formatLabelOverrides(req.LabelOverrides),
		formatERC20BalanceOverrides(req.ERC20BalanceOverrides),
		formatERC20ApprovalOverrides(req.ERC20ApprovalOverrides),
		formatERC721ApprovalOverrides(req.ERC721ApprovalOverrides),
		stateBytecode,
		req.Sender,
		req.Target,
		data,
	}
}

func ForgeCompilerArgs(config *model.CompilerConfig) []string {
	return forgeCompilerArgs(config, true)
}

func ForgeCompilerArgsExplicit(config *model.CompilerConfig) []string {
	return forgeCompilerArgs(config, false)
}

func forgeCompilerArgs(config *model.CompilerConfig, useDefaults bool) []string {
	if useDefaults {
		config = effectiveCompilerConfig(config)
	}
	if config == nil {
		return nil
	}

	args := make([]string, 0, 16)
	if config.NoAutoDetect {
		args = append(args, "--no-auto-detect")
	}
	if strings.TrimSpace(config.Use) != "" {
		args = append(args, "--use", strings.TrimSpace(config.Use))
	}
	if config.Offline {
		args = append(args, "--offline")
	}
	if config.ViaIR != nil && *config.ViaIR {
		args = append(args, "--via-ir")
	}
	if config.UseLiteralContent {
		args = append(args, "--use-literal-content")
	}
	if config.NoMetadata {
		args = append(args, "--no-metadata")
	}
	if strings.TrimSpace(config.EVMVersion) != "" {
		args = append(args, "--evm-version", strings.TrimSpace(config.EVMVersion))
	}
	if config.Optimize != nil {
		args = append(args, "--optimize="+fmt.Sprintf("%t", *config.Optimize))
	}
	if config.OptimizerRuns != nil {
		args = append(args, "--optimizer-runs", fmt.Sprintf("%d", *config.OptimizerRuns))
	}
	if strings.TrimSpace(config.RevertStrings) != "" {
		args = append(args, "--revert-strings", strings.TrimSpace(config.RevertStrings))
	}
	return args
}

func effectiveCompilerConfig(config *model.CompilerConfig) *model.CompilerConfig {
	viaIR := true
	optimize := true
	if config == nil {
		return &model.CompilerConfig{
			ViaIR:    &viaIR,
			Optimize: &optimize,
		}
	}

	effective := *config
	if effective.ViaIR == nil {
		effective.ViaIR = &viaIR
	}
	if effective.Optimize == nil {
		effective.Optimize = &optimize
	}
	return &effective
}

func formatLabelOverrides(items []model.LabelOverride) string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprintf("(%s,%s)", item.Account, formatStringArg(item.Label)))
	}
	return "[" + strings.Join(values, ",") + "]"
}

func formatERC20BalanceOverrides(items []model.ERC20BalanceOverride) string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprintf("(%s,%s,%s)", item.Token, item.Account, item.Balance.String()))
	}
	return "[" + strings.Join(values, ",") + "]"
}

func formatERC20ApprovalOverrides(items []model.ERC20ApprovalOverride) string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprintf("(%s,%s,%s,%s)", item.Token, item.Owner, item.Spender, item.Amount.String()))
	}
	return "[" + strings.Join(values, ",") + "]"
}

func formatERC721ApprovalOverrides(items []model.ERC721ApprovalOverride) string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, fmt.Sprintf("(%s,%s,%s,%s)", item.Token, item.Owner, item.Spender, item.TokenID.String()))
	}
	return "[" + strings.Join(values, ",") + "]"
}

func ValidateAddress(field string, value string) error {
	if !addressPattern.MatchString(value) {
		return fmt.Errorf("%s must be a 20-byte hex address", field)
	}
	return nil
}

func NormalizeBytes(field string, value string) (string, error) {
	if value == "" {
		return "0x", nil
	}
	if !strings.HasPrefix(value, "0x") && !strings.HasPrefix(value, "0X") {
		value = "0x" + value
	}
	if !bytesPattern.MatchString(value) {
		return "", fmt.Errorf("%s must be even-length hex bytes", field)
	}
	return "0x" + strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(value, "0x"), "0X")), nil
}

func formatStringArg(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func ContractIdentifier(repoRoot string, path string, contractName string) (string, error) {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("%s is outside repo root %s", path, repoRoot)
	}
	return filepath.ToSlash(rel) + ":" + contractName, nil
}

func DetectContractName(source string) string {
	match := contractNamePattern.FindStringSubmatch(source)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func ExtractBytecode(output string) (string, bool) {
	bytecode := strings.TrimSpace(output)
	if !bytesPattern.MatchString(bytecode) {
		matches := hexValuePattern.FindAllString(output, -1)
		if len(matches) > 0 {
			bytecode = matches[len(matches)-1]
		}
	}
	return bytecode, bytecode != "" && bytecode != "0x" && bytesPattern.MatchString(bytecode)
}

func StripANSI(value string) string {
	return ansiPattern.ReplaceAllString(value, "")
}

func RedactRPC(value string, rpcURL string, chain string) string {
	if rpcURL == "" {
		return value
	}
	return strings.ReplaceAll(value, rpcURL, "<rpc:"+chain+">")
}
