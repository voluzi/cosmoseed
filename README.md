# Cosmoseed

[![Test](https://github.com/voluzi/cosmoseed/actions/workflows/test.yml/badge.svg)](https://github.com/voluzi/cosmoseed/actions/workflows/test.yml)
[![GoReleaser](https://github.com/voluzi/cosmoseed/actions/workflows/goreleaser.yml/badge.svg)](https://github.com/voluzi/cosmoseed/actions/workflows/goreleaser.yml)
[![Docker Builds](https://github.com/voluzi/cosmoseed/actions/workflows/docker.yml/badge.svg)](https://github.com/voluzi/cosmoseed/actions/workflows/docker.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/voluzi/cosmoseed/blob/main/LICENSE.md)

**Cosmoseed** is a lightweight seed node for Cosmos-based blockchains.  
Unlike traditional seed implementations, Cosmoseed actively filters out unreachable or failing peers, ensuring that only viable peers are served to clients.

---

## 🚀 Features

- Acts as a dedicated **Cosmos seed node**
- Maintains a strict and adaptive **address book** of peers
- Fully configurable via `config.yaml` or command-line flags
- Lightweight, single-binary deployment
- Exposes a basic **HTTP endpoint** (`/peers`) to retrieve a randomized selection of known good peers

---

## 📦 Installation

To quickly install on linux or darwin you can run:

```bash
$ curl -s https://get.voluzi.com/cosmoseed! | bash
```

Alternatively, you can download the binary from releases or use the available docker image.

---

## 🛠 Configuration

By default, Cosmoseed reads its config from:

```bash
~/.cosmoseed/config.yaml
```

If it does not exist, Cosmoseed will create a new configuration file with the following defaults:

```yaml
nodeKeyFile: node_key.json
addrBookFile: addrbook.json
addrBookStrict: true
listenAddr: tcp://0.0.0.0:26656
logLevel: info
maxInboundPeers: 2000
maxOutboundPeers: 20
maxPacketMsgPayloadSize: 1024
peerQueueSize: 1000
dialWorkers: 20
chainID: ""
seeds: ""
apiAddr: 0.0.0.0:8080
```

Please note that `chainID` and `seeds` are required fields. You can either include them in the config file or pass them as command-line flags.

Once Cosmoseed starts and grabs the first peers, the address book will contain peers and `seeds` can then be omitted on following application starts.

---

## ⚙️ Command-Line Flags

```bash
$ cosmoseed --help

Usage of cosmoseed:
  -chain-id string
    	Chain ID to use
  -home string
    	Path to config/data directory (default "~/.cosmoseed")
  -log-level string
    	Logging level (default "info")
  -seeds string
    	Comma-separated list of seed peers
  -show-node-id
    	Print node ID and exit
  -version
    	Print version and exit
```

Flags take precedence over values defined in `config.yaml`.

---

## 🧪 Example Usage

```bash
$ cosmoseed \
  -chain-id nibiru-testnet-2 \
  -seeds c2f87136e1a8b1c4469ff5a65b6cb3d6aca2b5fd@34.79.42.220:26656
```

---

## ✨ Credits

Inspired by [`tenderseed`](https://github.com/binaryholdings/tenderseed) and the broader Cosmos ecosystem.
