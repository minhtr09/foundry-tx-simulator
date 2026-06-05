# Foundry Tx Simulator

Local transaction simulation and visualization tooling. The backend runs either a Foundry test harness or `cast-kyber run` and returns execution trace, fund-flow, and balance-analysis data; the frontend provides the local UI.

## Quick Start

Install Go:

```sh
brew install go
```

Install Node/Yarn with Volta:

```sh
curl https://get.volta.sh | bash
volta install node yarn
```

Install the pinned Kyber Foundry fork:

```sh
scripts/install-foundry-kyber.sh
```

The installer downloads the `thepluck/foundry` release
`nightly-04bdb43d9435cecec89abc17b3dcb618086c0d99`, verifies the release checksum,
and installs `forge-kyber`, `cast-kyber`, `anvil-kyber`, and `chisel-kyber` into
`~/.foundry/bin` by default. Make sure that directory is on your `PATH`, then
check:

```sh
forge-kyber --version
```

Create local config files from examples:

```sh
cp .env.example .env
cp config.example.yml config.yml
```

Fill in RPC URLs in `.env`, then start both servers:

```sh
./dev.sh
```

## Local Run

Run the backend and frontend together:

```sh
./dev.sh
```

After a run finishes, the UI shows its request ID. Paste that ID into the Request ID field later, or open a URL with `?requestId=<id>`, to reload the saved request and previous output from the backend record database.

Set local ports in `config.yml`; `./dev.sh` reads that file and points the frontend at the configured backend address.

Run the backend directly:

```sh
cd backend
go run ./cmd/server
```

Run the frontend directly:

```sh
cd frontend
yarn install
yarn dev
```

When run directly, the local frontend defaults to `http://127.0.0.1:8080` for the backend API.

## Configuration

App settings are read from YAML config. `config.yml` is the local config, and `config.example.yml` is the template for new configs. `./dev.sh` uses `config.yml` by default; direct backend runs from `backend/` find `../config.yml` automatically.

```yaml
listen_addr: "127.0.0.1:8080"
frontend_port: 5173
work_dir: "backend/.runs"
forge_bin: "forge-kyber"
cast_bin: "cast-kyber"
anvil_bin: "anvil-kyber"
anvil_port_start: 18545
rpc_urls:
  mainnet: "${MAINNET_RPC_URL}"
```

Set `TXSIM_CONFIG` only when you want to use a different YAML file:

```sh
cd backend
TXSIM_CONFIG=/path/to/config.yml go run ./cmd/server
```

The backend loads `.env` from the repo root and `backend/.env`. YAML config fields only use environment values when the YAML explicitly references them with `${...}`. For example, `MAINNET_RPC_URL` is applied because `config.yml` uses `${MAINNET_RPC_URL}` under `rpc_urls`; a plain `MAINNET_RPC_URL` environment variable does not override a literal YAML URL.

Use `.env.example` as the template for `.env`. Put secrets and machine-specific values in `.env`:

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

Use YAML fields such as `listen_addr`, `frontend_port`, `work_dir`, `max_concurrent_runs`, `anvil_port_start`, `rpc_urls`, `explorer_urls`, and `etherscan_api_key` for backend and `./dev.sh` settings. Runtime-only environment variables such as `COINGECKO_API_KEY` are still read directly by the code that needs them. `TXSIM_API_URL` is still available when running the frontend directly and the browser should call a specific backend URL.

Keep local `forge_bin` set to `forge-kyber`, `cast_bin` set to `cast-kyber`,
and `anvil_bin` set to `anvil-kyber`. Set request field `decodeInternal` to
`true` when you want the runner to add `--decode-internal`; the default is
`false`.

For local deployment without Docker:

```sh
(cd backend && go run ./cmd/server)
(cd frontend && TXSIM_API_URL=http://127.0.0.1:8080 yarn dev)
```

Local deployment stores saved simulation records in `backend/.runs/records.sqlite` and recently used Foundry project paths in `backend/.runs/projects.json` by default.

## Docker Run

Docker is optional and does not replace local deployment.

```sh
docker compose up --build
```

Then open:

- Frontend: `http://127.0.0.1:5173`
- Backend: `http://127.0.0.1:8080`
- Swagger UI: `http://127.0.0.1:8080/docs`

Docker stores recently used Foundry project paths in the `backend-runs` volume at `/data/runs/projects.json`, so project suggestions survive container rebuilds.
The backend image installs the pinned Kyber Foundry release and runs as
`linux/amd64`, matching the release's published Linux archive.

Override Docker host ports through `.env` or shell variables:

```sh
TXSIM_BACKEND_PORT=18080 TXSIM_FRONTEND_PORT=15173 docker compose up --build
```

The frontend container uses `TXSIM_BACKEND_PORT` to generate its browser runtime config, so the default API URL follows the published backend port. Set `TXSIM_API_URL` if the browser should call a different backend URL.

For external Foundry projects, Docker mounts the parent directory of this repo by default:

```text
.. -> /workspace/projects
```

That means a sibling project such as `~/Kyber/ks-dex-aggregator-sc` can still be entered in the UI as usual, and the backend will resolve it to `/workspace/projects/ks-dex-aggregator-sc` inside the container. If your projects live somewhere else, set:

```sh
TXSIM_PROJECTS_HOST_PATH=~/Kyber
TXSIM_PROJECTS_CONTAINER_PATH=/workspace/projects
```

The backend expands `~` in `projectPath` and `project_roots`. Docker Compose also resolves `~` in `TXSIM_PROJECTS_HOST_PATH`. The native folder picker remains a local macOS backend feature and is not available inside the Linux container, so Docker users should type or paste the project path.

## Release

Create and push a version tag from `master` to run the release workflow:

```sh
git checkout master
git pull --ff-only origin master
git tag v0.1.0
git push origin v0.1.0
```

The workflow checks the existing CI status on the tagged merge commit before publishing release artifacts. It uploads backend server archives for Linux and macOS, a frontend `dist` archive, and `SHA256SUMS`.

Use the manual Release workflow dispatch only when re-running release creation for an existing `v*` tag after deleting the existing GitHub release for that tag.

## Foundry Contracts

The local simulation test harness and its Foundry project live in `contracts/`.

### Build

```shell
$ cd contracts
$ forge-kyber build
```

### Test

```shell
$ cd contracts
$ forge-kyber test
```

### Format

```shell
$ cd contracts
$ forge-kyber fmt
```

You can also run from the repo root by passing `--root contracts`:

```shell
$ forge-kyber build --root contracts
$ forge-kyber test --root contracts
```
