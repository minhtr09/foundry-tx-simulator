package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadExpandsAndNormalizesExplorerURLs(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	workDir := filepath.Join(configDir, "runs")

	t.Setenv("TXSIM_CONFIG", configPath)
	t.Setenv("MAINNET_RPC_URL", "https://rpc.example")
	t.Setenv("MAINNET_EXPLORER_URL", "https://explorer.example/")
	t.Setenv("PROJECT_ROOT", filepath.Join(configDir, "projects"))

	data := []byte(`listen_addr: "127.0.0.1:0"
repo_root: "."
project_roots:
  - "${PROJECT_ROOT}"
work_dir: "` + filepath.ToSlash(workDir) + `"
timeout_seconds: 1
max_concurrent_runs: 1
forge_bin: "forge-kyber"
rpc_urls:
  mainnet: "${MAINNET_RPC_URL}"
explorer_urls:
  mainnet: "${MAINNET_EXPLORER_URL}"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, gotPath, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if gotPath != configPath {
		t.Fatalf("config path = %q, want %q", gotPath, configPath)
	}
	if cfg.RPCURLs["mainnet"] != "https://rpc.example" {
		t.Fatalf("mainnet rpc = %q", cfg.RPCURLs["mainnet"])
	}
	if cfg.ExplorerURLs["mainnet"] != "https://explorer.example" {
		t.Fatalf("mainnet explorer = %q", cfg.ExplorerURLs["mainnet"])
	}
	if len(cfg.ProjectRoots) != 1 || cfg.ProjectRoots[0] != filepath.Join(configDir, "projects") {
		t.Fatalf("project roots = %#v", cfg.ProjectRoots)
	}
	if cfg.ProjectCachePath != filepath.Join(workDir, "projects.json") {
		t.Fatalf("project cache path = %q, want default under work dir", cfg.ProjectCachePath)
	}
}

func TestResolveConfigPathPrefersRootConfigFromRepoRoot(t *testing.T) {
	rootDir := t.TempDir()
	rootConfig := filepath.Join(rootDir, "config.yml")
	if err := os.WriteFile(rootConfig, []byte("rpc_urls: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveConfigPath(rootDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != rootConfig {
		t.Fatalf("config path = %q, want root config %q", got, rootConfig)
	}
}

func TestResolveConfigPathFindsRootConfigFromBackendWorkingDirectory(t *testing.T) {
	rootDir := t.TempDir()
	backendDir := filepath.Join(rootDir, "backend")
	if err := os.Mkdir(backendDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootConfig := filepath.Join(rootDir, "config.yml")
	if err := os.WriteFile(rootConfig, []byte("rpc_urls: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(backendDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldDir)
	})

	got, err := ResolveConfigPath("", "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "../config.yml" {
		t.Fatalf("config path = %q, want parent root config candidate", got)
	}
}

func TestResolveConfigPathReportsAcceptedConfigNames(t *testing.T) {
	_, err := ResolveConfigPath(t.TempDir(), "")
	if err == nil {
		t.Fatal("ResolveConfigPath succeeded, want error")
	}
	for _, want := range []string{"config.yml", "config.yaml", "config.example.yml", "config.example.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to mention %q", err.Error(), want)
		}
	}
}

func TestLoadUsesListenAddressFromConfigDespiteEnv(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")

	t.Setenv("TXSIM_CONFIG", configPath)
	t.Setenv("TXSIM_LISTEN_ADDR", "127.0.0.1:9090")

	data := []byte(`listen_addr: "127.0.0.1:8080"
repo_root: "."
rpc_urls:
  mainnet: "https://rpc.example"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("listen addr = %q, want config value", cfg.ListenAddr)
	}
}

func TestLoadUsesConfigValuesDespiteEnv(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")
	workDir := filepath.Join(configDir, "env-runs")

	t.Setenv("TXSIM_CONFIG", configPath)
	t.Setenv("TXSIM_LISTEN_ADDR", "127.0.0.1:9090")
	t.Setenv("TXSIM_FRONTEND_PORT", "15173")
	t.Setenv("TXSIM_WORK_DIR", workDir)
	t.Setenv("TXSIM_TIMEOUT_SECONDS", "9")
	t.Setenv("TXSIM_MAX_CONCURRENT_RUNS", "3")
	t.Setenv("TXSIM_FORGE_BIN", "forge-env")
	t.Setenv("TXSIM_ANVIL_BIN", "anvil-env")
	t.Setenv("TXSIM_ANVIL_HOST", "127.0.0.2")
	t.Setenv("TXSIM_ANVIL_PORT_START", "19454")
	t.Setenv("TXSIM_ETHERSCAN_API_KEY", "etherscan-env")
	t.Setenv("MAINNET_RPC_URL", "https://rpc.env")
	t.Setenv("MAINNET_EXPLORER_URL", "https://explorer.env/")

	data := []byte(`listen_addr: "127.0.0.1:8080"
frontend_port: 5174
repo_root: "."
work_dir: "config-runs"
timeout_seconds: 1
max_concurrent_runs: 1
forge_bin: "forge-config"
anvil_bin: "anvil-config"
anvil_host: "127.0.0.1"
anvil_port_start: 18545
etherscan_api_key: "etherscan-config"
rpc_urls:
  mainnet: "https://rpc.config"
explorer_urls:
  mainnet: "https://explorer.config"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("listen addr = %q, want config value", cfg.ListenAddr)
	}
	if cfg.FrontendPort != 5174 {
		t.Fatalf("frontend port = %d, want config value", cfg.FrontendPort)
	}
	if cfg.WorkDir != filepath.Join(configDir, "config-runs") {
		t.Fatalf("work dir = %q, want config value", cfg.WorkDir)
	}
	if cfg.TimeoutSeconds != 1 {
		t.Fatalf("timeout seconds = %d, want config value", cfg.TimeoutSeconds)
	}
	if cfg.MaxConcurrent != 1 {
		t.Fatalf("max concurrent = %d, want config value", cfg.MaxConcurrent)
	}
	if cfg.ForgeBin != "forge-config" {
		t.Fatalf("forge bin = %q, want config value", cfg.ForgeBin)
	}
	if cfg.AnvilBin != "anvil-config" {
		t.Fatalf("anvil bin = %q, want config value", cfg.AnvilBin)
	}
	if cfg.AnvilHost != "127.0.0.1" {
		t.Fatalf("anvil host = %q, want config value", cfg.AnvilHost)
	}
	if cfg.AnvilPortStart != 18545 {
		t.Fatalf("anvil port start = %d, want config value", cfg.AnvilPortStart)
	}
	if cfg.EtherscanAPIKey != "etherscan-config" {
		t.Fatalf("etherscan api key = %q, want config value", cfg.EtherscanAPIKey)
	}
	if cfg.RPCURLs["mainnet"] != "https://rpc.config" {
		t.Fatalf("mainnet rpc = %q, want config value", cfg.RPCURLs["mainnet"])
	}
	if cfg.ExplorerURLs["mainnet"] != "https://explorer.config" {
		t.Fatalf("mainnet explorer = %q, want config value", cfg.ExplorerURLs["mainnet"])
	}
}

func TestLoadReadsDotEnvWithGotenv(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")

	t.Setenv("TXSIM_CONFIG", configPath)
	t.Cleanup(func() {
		_ = os.Unsetenv("ETHERSCAN_API_KEY")
	})

	data := []byte(`repo_root: "."
rpc_urls:
  mainnet: "${MAINNET_RPC_URL}"
explorer_urls:
  mainnet: "${MAINNET_EXPLORER_URL}"
etherscan_api_key: "${ETHERSCAN_API_KEY}"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	dotenv := []byte(`MAINNET_RPC_URL="https://rpc.dotenv"
MAINNET_EXPLORER_URL=https://explorer.dotenv/
ETHERSCAN_API_KEY=etherscan-dotenv
`)
	if err := os.WriteFile(filepath.Join(configDir, ".env"), dotenv, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RPCURLs["mainnet"] != "https://rpc.dotenv" {
		t.Fatalf("mainnet rpc = %q, want dotenv value", cfg.RPCURLs["mainnet"])
	}
	if cfg.ExplorerURLs["mainnet"] != "https://explorer.dotenv" {
		t.Fatalf("mainnet explorer = %q, want dotenv value", cfg.ExplorerURLs["mainnet"])
	}
	if cfg.EtherscanAPIKey != "etherscan-dotenv" {
		t.Fatalf("etherscan api key = %q, want dotenv value", cfg.EtherscanAPIKey)
	}
}

func TestLoadExistingEnvOverridesDotEnv(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")

	t.Setenv("TXSIM_CONFIG", configPath)
	t.Setenv("MAINNET_RPC_URL", "https://rpc.env")

	data := []byte(`repo_root: "."
rpc_urls:
  mainnet: "${MAINNET_RPC_URL}"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, ".env"), []byte(`MAINNET_RPC_URL=https://rpc.dotenv`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RPCURLs["mainnet"] != "https://rpc.env" {
		t.Fatalf("mainnet rpc = %q, want existing env placeholder value", cfg.RPCURLs["mainnet"])
	}
}

func TestLoadDoesNotReadPlainEtherscanAPIKeyEnvWithoutConfigPlaceholder(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")

	t.Setenv("TXSIM_CONFIG", configPath)
	t.Setenv("ETHERSCAN_API_KEY", "etherscan-env")

	data := []byte(`repo_root: "."
rpc_urls:
  mainnet: "https://rpc.example"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EtherscanAPIKey != "" {
		t.Fatalf("etherscan api key = %q, want empty value", cfg.EtherscanAPIKey)
	}
}

func TestLoadUsesViperDefaults(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")

	t.Setenv("TXSIM_CONFIG", configPath)

	data := []byte(`rpc_urls:
  mainnet: "https://rpc.example"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("listen addr = %q, want viper default", cfg.ListenAddr)
	}
	if cfg.FrontendPort != 5173 {
		t.Fatalf("frontend port = %d, want viper default", cfg.FrontendPort)
	}
	if cfg.RepoRoot != configDir {
		t.Fatalf("repo root = %q, want viper default normalized", cfg.RepoRoot)
	}
	if cfg.WorkDir != filepath.Join(configDir, "backend", ".runs") {
		t.Fatalf("work dir = %q, want viper default normalized", cfg.WorkDir)
	}
	if cfg.ProjectCachePath != filepath.Join(configDir, "backend", ".runs", "projects.json") {
		t.Fatalf("project cache path = %q, want default under work dir", cfg.ProjectCachePath)
	}
	if cfg.TimeoutSeconds != 300 {
		t.Fatalf("timeout seconds = %d, want viper default", cfg.TimeoutSeconds)
	}
	if cfg.MaxConcurrent != 1 {
		t.Fatalf("max concurrent = %d, want viper default", cfg.MaxConcurrent)
	}
	if cfg.ForgeBin != "forge-kyber" {
		t.Fatalf("forge bin = %q, want viper default", cfg.ForgeBin)
	}
	if cfg.AnvilBin != "anvil-kyber" {
		t.Fatalf("anvil bin = %q, want viper default", cfg.AnvilBin)
	}
	if cfg.AnvilHost != "127.0.0.1" {
		t.Fatalf("anvil host = %q, want viper default", cfg.AnvilHost)
	}
	if cfg.AnvilPortStart != 18545 {
		t.Fatalf("anvil port start = %d, want viper default", cfg.AnvilPortStart)
	}
	if cfg.EtherscanAPIKey != "" {
		t.Fatalf("etherscan api key = %q, want empty viper default", cfg.EtherscanAPIKey)
	}
}

func TestLoadExpandsHomeInProjectRoots(t *testing.T) {
	configDir := t.TempDir()
	homeDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")

	t.Setenv("HOME", homeDir)
	t.Setenv("TXSIM_CONFIG", configPath)

	data := []byte(`repo_root: "."
project_roots:
  - "~/projects"
rpc_urls:
  mainnet: "https://rpc.example"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(homeDir, "projects")
	if len(cfg.ProjectRoots) != 1 || cfg.ProjectRoots[0] != want {
		t.Fatalf("project roots = %#v, want %q", cfg.ProjectRoots, want)
	}
}

func TestLoadNormalizesProjectCachePath(t *testing.T) {
	configDir := t.TempDir()
	configPath := filepath.Join(configDir, "config.yaml")

	t.Setenv("TXSIM_CONFIG", configPath)

	data := []byte(`repo_root: "."
project_cache_path: ".cache/projects.json"
rpc_urls:
  mainnet: "https://rpc.example"
`)
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(configDir, ".cache", "projects.json")
	if cfg.ProjectCachePath != want {
		t.Fatalf("project cache path = %q, want %q", cfg.ProjectCachePath, want)
	}
}
