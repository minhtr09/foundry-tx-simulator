# Foundry Tx Simulator Backend

Local Go server that accepts simulation parameters, maps `chain` to an RPC URL from config, compiles optional state override Solidity, runs the existing Foundry script with CLI arguments, and returns the raw Forge JSON trace with backend-derived fund-flow and balance analysis.

## Run

```sh
cd backend
go run ./cmd/server
```

To log HTTP request and response bodies while debugging:

```sh
cd backend
TXSIM_DEBUG_HTTP=1 go run ./cmd/server
```

The backend also logs simulation stages with `slog`: worker acquisition, Foundry project setup, external `forge build src`, state override compilation, Anvil start/reset, Forge script exit code, parsed trace size, transfer count, price-fetch count, and balance-analysis count. These logs avoid printing upstream RPC URLs, Etherscan API keys, calldata, or state override bytecode.

Docker is available as an optional deployment path from the repo root:

```sh
docker compose up --build backend
```

The Docker backend listens on `0.0.0.0:8080` inside the container and is published to `http://127.0.0.1:8080` by `docker-compose.yml`. Local `go run` deployment is unchanged.

Override the Docker host port with `TXSIM_BACKEND_PORT`:

```sh
TXSIM_BACKEND_PORT=18080 docker compose up --build backend
```

Set the local backend listen address in YAML:

```yaml
listen_addr: "127.0.0.1:18080"
```

The server loads config from `TXSIM_CONFIG` when set. Otherwise it searches from the current working directory for `config.yml`, `config.yaml`, `config.example.yml`, and `config.example.yaml`; direct `go run` commands from `backend/` also check the same names in `..`. `./dev.sh` uses repo-root `config.yml` by default.

Use repo-root `config.yml` for local development or `config.example.yaml` as a template for another config file. The backend loads `.env` from the repo root and `backend/.env` with `gotenv`. YAML config fields only use environment values when the YAML explicitly references them with `${...}`.

Use the repo-root `.env.example` as the template for `.env`. Put secrets and machine-specific values in `.env`:

```env
MAINNET_RPC_URL=https://mainnet.example
BASE_RPC_URL=https://base.example
ARBITRUM_RPC_URL=https://arbitrum.example
ETHERSCAN_API_KEY=...
COINGECKO_API_KEY=...
```

Then reference them from YAML:

```yaml
etherscan_api_key: "${ETHERSCAN_API_KEY}"
rpc_urls:
  mainnet: "${MAINNET_RPC_URL}"
```

Backend runtime settings and `./dev.sh` settings live in YAML:

```yaml
listen_addr: "127.0.0.1:8080"
frontend_port: 5173
work_dir: "backend/.runs"
project_cache_path: "backend/.runs/projects.json"
timeout_seconds: 300
max_concurrent_runs: 1
forge_bin: "forge"
anvil_bin: "anvil"
anvil_host: "127.0.0.1"
anvil_port_start: 18545
etherscan_api_key: "${ETHERSCAN_API_KEY}"
rpc_urls:
  mainnet: "${MAINNET_RPC_URL}"
explorer_urls:
  mainnet: "https://etherscan.io"
```

Chain RPC endpoints are read from the YAML `rpc_urls` map. `explorer_urls` maps the same chain names to block explorer base URLs for frontend address links. `etherscan_api_key` is backend-side only and maps to `forge script --etherscan-api-key`; set it directly in YAML or use `${ETHERSCAN_API_KEY}`. `frontend_port` controls the Vite server when using `./dev.sh`. Set `COINGECKO_API_KEY` in `.env` if you want CoinGecko requests to include a demo API key.

Simulation records are stored in a SQLite database at `<work_dir>/records.sqlite`. `project_cache_path` stores recently used Foundry project paths. Local runs default to `backend/.runs/projects.json`; Docker uses `/data/runs` for records and project cache data, and persists that directory with the `backend-runs` volume.

`max_concurrent_runs` controls the simulation worker pool size. Each worker lazily starts one quiet Anvil fork on a distinct port, reuses it across requests, and resets it with `anvil_reset` before later runs. `anvil_bin`, `anvil_host`, and `anvil_port_start` configure the local fork processes. Keep concurrency at `1` for the safest local behavior, or raise it if your machine/RPC can handle parallel simulations.

## Endpoints

- `GET /docs`
- `GET /openapi.json`
- `GET /health`
- `GET /chains`
- `GET /projects`
- `GET /browse/project`
- `POST /simulate`

`GET /projects` returns cached Foundry project paths in most-recent-first order. The frontend uses it for Foundry Project suggestions.

`GET /browse/project` opens a native local folder picker and returns the selected project path. It is intended for the local frontend's Foundry Project browse button.

Inside Docker, native project browsing is unavailable because the backend runs in a Linux container. Type or paste `projectPath` manually. The Compose stack mounts `TXSIM_PROJECTS_HOST_PATH` to `TXSIM_PROJECTS_CONTAINER_PATH`, and the backend can resolve host-style paths against that mounted project root. `~` is supported in `projectPath` and configured project roots.

## Simulate Request

```json
{
  "chain": "mainnet",
  "blockNumber": "23000000",
  "projectPath": "~/foundry-project",
  "labelOverrides": [
    {
      "account": "0x0000000000000000000000000000000000000000",
      "label": "ExampleAccount"
    }
  ],
  "erc20BalanceOverrides": [
    {
      "token": "0x0000000000000000000000000000000000000000",
      "account": "0x0000000000000000000000000000000000000000",
      "balance": "1000000000000000000"
    }
  ],
  "erc20ApprovalOverrides": [
    {
      "token": "0x0000000000000000000000000000000000000000",
      "owner": "0x0000000000000000000000000000000000000000",
      "spender": "0x0000000000000000000000000000000000000000",
      "amount": "1000000000000000000"
    }
  ],
  "erc721ApprovalOverrides": [
    {
      "token": "0x0000000000000000000000000000000000000000",
      "owner": "0x0000000000000000000000000000000000000000",
      "spender": "0x0000000000000000000000000000000000000000",
      "tokenId": "1"
    }
  ],
  "stateOverride": {
    "contractName": "MyStateOverride",
    "source": "// SPDX-License-Identifier: UNLICENSED\npragma solidity ^0.8.0;\ncontract MyStateOverride { fallback() external {} }"
  },
  "compiler": {
    "viaIR": true,
    "optimize": true,
    "optimizerRuns": 200,
    "revertStrings": "default"
  },
  "sender": "0x0000000000000000000000000000000000000000",
  "target": "0x0000000000000000000000000000000000000000",
  "data": "0x"
}
```

`blockNumber`, balances, approvals, and token IDs should be strings when they may exceed JavaScript's safe integer range. Hex strings are accepted for uint fields.

`stateOverride` is optional. When provided, the backend writes the source into the per-request work directory for local runs, or into a temporary file under `<projectPath>/script/` for external-project runs. It then runs `forge inspect <file>:<contractName> bytecode`, passes that creation bytecode as a `run(...)` argument, and executes the simulation script with `forge script --json`.

`chain` selects the upstream fork RPC from config, and `blockNumber` selects the fork block. The backend prepares a worker-owned Anvil instance with those fork settings, then runs Forge against the local Anvil RPC. They are not part of the Solidity script signature.

`projectPath` is optional. When provided, the backend treats it as another Foundry project, runs `forge build src --root <projectPath>`, copies `contracts/src/SimulateTx.s.sol` into a deterministic content-hash file under `<projectPath>/script/`, runs `forge script --json` against that copied script with `--root <projectPath>`, then removes the temporary script file after the last active run using it finishes. Relative paths are resolved against the backend repo root. Paths beginning with `~` are expanded to the backend process user's home directory before validation.

`compiler` is optional and maps to popular Forge compiler flags. Supported fields are `use`, `offline`, `noAutoDetect`, `viaIR`, `useLiteralContent`, `noMetadata`, `evmVersion`, `optimize`, `optimizerRuns`, and `revertStrings`. The backend only passes `use` and `evmVersion` when they are explicitly provided. The state override `forge inspect` compile and final `forge script` run default `viaIR` and `optimize` to `true`; external-project `forge build src` uses the target project's defaults unless compiler fields are explicitly set.

The response includes `erc20Transfers`, parsed directly from ERC20 `Transfer` logs in the Forge JSON arena for later fund flow graph construction. Logs emitted from delegatecall frames are attributed by walking up the arena parents to the nearest `CALL` frame, so proxy token transfers use the proxy address rather than the implementation address. Each item contains `token`, `from`, `to`, raw `amount`, and, when metadata is available, `normalizedAmount`, `symbol`, and `logoUrl`.

The response also includes `balanceAnalysis`, which aggregates ERC20 transfers into signed per-user token balance changes. It fetches token decimals and symbols from the configured chain RPC, gets current USD prices from DefiLlama and CoinGecko, and may use DexScreener only for token display metadata such as symbol/logo. Trust Wallet token logo URLs are used as a fallback when the token address can be checksummed. USD values are only calculated when both a price and token decimals are available.

Each started simulation is saved in `<work_dir>/records.sqlite`. The response `id` can be used later to reload the exact request and display its previous output:

```sh
curl http://127.0.0.1:8080/requests/20260511T120000.000000000-deadbeef
```

The lookup response has this shape:

```json
{
  "id": "20260511T120000.000000000-deadbeef",
  "request": {},
  "response": {}
}
```
