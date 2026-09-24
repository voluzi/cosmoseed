# Cosmoseed

[![Test](https://github.com/voluzi/cosmoseed/actions/workflows/test.yml/badge.svg)](https://github.com/voluzi/cosmoseed/actions/workflows/test.yml)
[![GoReleaser](https://github.com/voluzi/cosmoseed/actions/workflows/goreleaser.yml/badge.svg)](https://github.com/voluzi/cosmoseed/actions/workflows/goreleaser.yml)
[![Docker Builds](https://github.com/voluzi/cosmoseed/actions/workflows/docker.yml/badge.svg)](https://github.com/voluzi/cosmoseed/actions/workflows/docker.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/voluzi/cosmoseed/blob/main/LICENSE.md)

Cosmoseed is a Cosmos seed node that serves peers only after an authenticated outbound P2P connection. The address book remains a pool of candidates; an old "good" mark or an inbound connection does not make a peer eligible for HTTP or PEX responses. Verification expires after `verificationTTL`, including when rechecks are delayed. A restart begins with no verified peers.

## Install and run

Download a binary from [releases](https://github.com/voluzi/cosmoseed/releases), or build locally with `make build`. The container image is `ghcr.io/voluzi/cosmoseed`. Pushes to `main` publish the multi-architecture `:edge` image. Stable version tags publish their semver tag and `:latest`; prerelease tags publish only their semver tag. Production deployments should pin a version tag or digest.

Copy [config.example.yaml](config.example.yaml) to `~/.cosmoseed/config.yaml`, set `chainID`, and optionally add known seeds. Then run `cosmoseed`. Config resolution is defaults → YAML → explicitly present environment variables → explicitly supplied flags. Only `CHAIN_ID`, `SEEDS`, `LOG_LEVEL`, `EXTERNAL_ADDRESS`, `POD_NAME`, and `HOME_DIR` have environment overrides. Unknown YAML fields are rejected. The effective config is saved atomically with mode `0600`, unless `--config-read-only` is set.

`--show-node-id` loads or creates only the node key and prints its ID. It works without `chainID`, does not bind listeners, and does not rewrite `config.yaml`.

For a one-off invocation:

```sh
cosmoseed --chain-id my-chain --seeds 'nodeid@seed.example:26656'
```

`--external-address` accepts a host and port, including `[2001:db8::1]:26656` for IPv6. With `--pod-name` or `POD_NAME`, a comma-separated external address list must contain the address at that pod's zero-based ordinal; a single address is also accepted for any ordinal. Invalid or missing list entries fail startup. Pod names also select a distinct node key file. An explicit `--home` wins over `HOME_DIR`.

## Endpoints

The API listens on `apiAddr` (default `0.0.0.0:8080`):

| Path | Response |
| --- | --- |
| `/` | This seed's `nodeID@host:port` |
| `/peers` | Comma-separated verified peer addresses, for existing clients |
| `/peers?limit=20&format=json` | JSON array of address, ID, IP, port, verified, last verification time, and age in seconds |
| `/peers?format=toml` | Paste-ready `persistent_peers = "..."` |
| `/healthz` | Process liveness |
| `/readyz` | Running and at least `minReadyPeers` fresh verified peers (default 1) |
| `/status` | Version, chain ID, readiness, verified and candidate counts |

`limit` is 1–100. `verified=false` is rejected; the API cannot serve unverified candidates. The default JSON empty result is `[]`. `/status` and metrics report the full fresh verified count even when peer responses are capped at 100. Metrics are exposed only on `metricsAddr` (default `127.0.0.1:9090`) at `/metrics`; set it to empty to disable the listener. Each seed instance has its own registry. The chart intentionally binds metrics to `0.0.0.0:9090` for its metrics Service.

## Kubernetes

The [Helm chart](charts/cosmoseed) uses a persistent StatefulSet, one PVC per replica, a headless P2P service, separate API and metrics services, health probes, and restrictive pod security settings. Set `config.chainID` and choose a reachable address per replica in `config.externalAddresses` when publishing seeds outside the cluster. With those addresses configured, the chart creates one P2P Service per pod; each Service selects only that pod, avoiding a load-balanced node-ID endpoint. Each P2P Service exposes the port in its corresponding external address (for example, `seed.example:443` exposes 443) and forwards to the pod's internal `ports.p2p`; make the external address reachable on that public port. The metrics Service publishes pod addresses before readiness so scrapes can show startup state; the API Service waits for readiness. Chart and app versions are independent: a chart-only change increments `version`, while `appVersion` names the default image tag. ServiceMonitor is optional and selects only the metrics Service.

If supplying `existingConfigMap`, also set `existingConfigMapChecksum` to a value that changes whenever that ConfigMap's contents change; the chart uses it to trigger a rollout.

```sh
helm template seed charts/cosmoseed --set config.chainID=my-chain
```

See [v0.12 migration notes](docs/migration-v0.12.md) before upgrading an existing seed.

Inspired by [`tenderseed`](https://github.com/binaryholdings/tenderseed) and the Cosmos ecosystem.
