package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"github.com/subosito/gotenv"

	"foundry-tx-simulator/backend/internal/pathutil"
)

type Config struct {
	ListenAddr       string            `mapstructure:"listen_addr"`
	FrontendPort     int               `mapstructure:"frontend_port"`
	RepoRoot         string            `mapstructure:"repo_root"`
	ProjectRoots     []string          `mapstructure:"project_roots"`
	WorkDir          string            `mapstructure:"work_dir"`
	ProjectCachePath string            `mapstructure:"project_cache_path"`
	TimeoutSeconds   int               `mapstructure:"timeout_seconds"`
	MaxConcurrent    int               `mapstructure:"max_concurrent_runs"`
	ForgeBin         string            `mapstructure:"forge_bin"`
	AnvilBin         string            `mapstructure:"anvil_bin"`
	AnvilHost        string            `mapstructure:"anvil_host"`
	AnvilPortStart   int               `mapstructure:"anvil_port_start"`
	EtherscanAPIKey  string            `mapstructure:"etherscan_api_key"`
	RPCURLs          map[string]string `mapstructure:"rpc_urls"`
	ExplorerURLs     map[string]string `mapstructure:"explorer_urls"`
}

const (
	DefaultListenHost   = "127.0.0.1"
	DefaultBackendPort  = 8080
	DefaultListenAddr   = "127.0.0.1:8080"
	DefaultFrontendPort = 5173
)

var configCandidates = []string{
	"config.yml",
	"config.yaml",
	"config.example.yml",
	"config.example.yaml",
}

var parentConfigCandidates = []string{
	"../config.yml",
	"../config.yaml",
	"../config.example.yml",
	"../config.example.yaml",
}

func Load() (Config, string, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return Config{}, "", err
	}
	return LoadFile(path)
}

func LoadFile(path string) (Config, string, error) {
	v := newConfigViper(path)
	if err := v.ReadInConfig(); err != nil {
		return Config{}, "", err
	}

	configPath, err := filepath.Abs(v.ConfigFileUsed())
	if err != nil {
		return Config{}, "", err
	}
	configDir := filepath.Dir(configPath)
	if err := loadDotEnvForConfig(v, configDir); err != nil {
		return Config{}, "", err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, "", err
	}
	cfg.ListenAddr = strings.TrimSpace(cfg.ListenAddr)
	cfg.RepoRoot, err = normalizeConfigPath(configDir, cfg.RepoRoot)
	if err != nil {
		return Config{}, "", err
	}
	cfg.ProjectRoots, err = normalizeConfigPaths(configDir, cfg.ProjectRoots)
	if err != nil {
		return Config{}, "", err
	}

	if !filepath.IsAbs(cfg.WorkDir) {
		cfg.WorkDir = filepath.Join(configDir, cfg.WorkDir)
	}
	cfg.WorkDir, err = filepath.Abs(cfg.WorkDir)
	if err != nil {
		return Config{}, "", err
	}
	if cfg.ProjectCachePath == "" {
		cfg.ProjectCachePath = filepath.Join(cfg.WorkDir, "projects.json")
	} else {
		cfg.ProjectCachePath, err = normalizeConfigPath(configDir, cfg.ProjectCachePath)
		if err != nil {
			return Config{}, "", err
		}
	}

	if cfg.TimeoutSeconds < 0 {
		return Config{}, "", errors.New("timeout_seconds must be positive")
	}
	if cfg.MaxConcurrent < 0 {
		return Config{}, "", errors.New("max_concurrent_runs must be positive")
	}
	if cfg.FrontendPort <= 0 {
		return Config{}, "", errors.New("frontend_port must be positive")
	}
	cfg.ForgeBin = strings.TrimSpace(cfg.ForgeBin)
	cfg.AnvilBin = strings.TrimSpace(cfg.AnvilBin)
	cfg.AnvilHost = strings.TrimSpace(cfg.AnvilHost)
	cfg.EtherscanAPIKey = strings.TrimSpace(os.ExpandEnv(cfg.EtherscanAPIKey))
	if cfg.AnvilPortStart < 0 {
		return Config{}, "", errors.New("anvil_port_start must be positive")
	}
	if len(cfg.RPCURLs) == 0 {
		return Config{}, "", errors.New("rpc_urls must contain at least one chain")
	}
	cfg.RPCURLs = resolveRPCURLs(cfg.RPCURLs)
	if cfg.ExplorerURLs == nil {
		cfg.ExplorerURLs = map[string]string{}
	}
	cfg.ExplorerURLs = resolveExplorerURLs(cfg.ExplorerURLs)

	return cfg, configPath, nil
}

func resolveConfigPath() (string, error) {
	return ResolveConfigPath("", os.Getenv("TXSIM_CONFIG"))
}

func ResolveConfigPath(baseDir string, configured string) (string, error) {
	configured = strings.TrimSpace(configured)
	if configured != "" {
		candidate, err := pathutil.ExpandHome(configured)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(candidate) && strings.TrimSpace(baseDir) != "" {
			candidate = filepath.Join(baseDir, candidate)
		}
		if stat, err := os.Stat(candidate); err == nil && !stat.IsDir() {
			return candidate, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		return "", fmt.Errorf("TXSIM_CONFIG points to missing config: %s", candidate)
	}

	candidates := configCandidates
	if strings.TrimSpace(baseDir) == "" {
		candidates = append(append([]string{}, configCandidates...), parentConfigCandidates...)
	}
	for _, candidate := range candidates {
		path := candidate
		if strings.TrimSpace(baseDir) != "" {
			path = filepath.Join(baseDir, candidate)
		}
		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			return path, nil
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("set TXSIM_CONFIG or create one of: %s", strings.Join(candidates, ", "))
}

func newConfigViper(path string) *viper.Viper {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetDefault("listen_addr", DefaultListenAddr)
	v.SetDefault("frontend_port", DefaultFrontendPort)
	v.SetDefault("repo_root", ".")
	v.SetDefault("work_dir", "backend/.runs")
	v.SetDefault("timeout_seconds", 300)
	v.SetDefault("max_concurrent_runs", 1)
	v.SetDefault("forge_bin", "forge")
	v.SetDefault("anvil_bin", "anvil")
	v.SetDefault("anvil_host", "127.0.0.1")
	v.SetDefault("anvil_port_start", 18545)
	v.SetDefault("etherscan_api_key", "")
	return v
}

func loadDotEnvForConfig(v *viper.Viper, configDir string) error {
	repoRoot, err := normalizeConfigPath(configDir, v.GetString("repo_root"))
	if err != nil {
		return err
	}
	return loadDotEnv(filepath.Join(repoRoot, ".env"), filepath.Join(configDir, ".env"))
}

func normalizeConfigPaths(baseDir string, values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized, err := normalizeConfigPath(baseDir, os.ExpandEnv(value))
		if err != nil {
			return nil, err
		}
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeConfigPath(baseDir string, value string) (string, error) {
	value = strings.TrimSpace(os.ExpandEnv(value))
	if value == "" {
		return "", nil
	}
	return pathutil.AbsFrom(baseDir, value)
}

func loadDotEnv(paths ...string) error {
	existing := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		if _, ok := seen[absPath]; ok {
			continue
		}
		seen[absPath] = struct{}{}

		if _, err := os.Stat(absPath); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return err
		}
		existing = append(existing, absPath)
	}
	if len(existing) == 0 {
		return nil
	}
	return gotenv.Load(existing...)
}

func resolveRPCURLs(values map[string]string) map[string]string {
	resolved := make(map[string]string, len(values))
	for chain, rpcURL := range values {
		resolved[chain] = os.ExpandEnv(rpcURL)
	}
	return resolved
}

func resolveExplorerURLs(values map[string]string) map[string]string {
	resolved := make(map[string]string, len(values))
	for chain, explorerURL := range values {
		resolved[chain] = strings.TrimRight(os.ExpandEnv(explorerURL), "/")
	}
	return resolved
}
