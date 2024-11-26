# Dave CLI

Dave is a distributed key-value store built on UDP, featuring proof-of-work data prioritization and trust-based peer selection.

## Installation

```bash
go install github.com/intob/daved
```

## Quick Start

Generate a new key pair:
```bash
dave keygen
```

Start a node:
```bash
dave -udp_listen_addr "[::]:8000"
```

## Configuration

**Command Line Flags**

| Flag | Description | Default |
|------|-------------|---------|
| `-cfg` | Config filename | "" |
| `-data_key_filename` | Data private key file | "key.dave" |
| `-d` | Proof-of-work difficulty (zero bits) | 16 |
| `-udp_listen_addr` | Listen address:port | "[::]:127" |
| `-edges` | Comma-separated bootstrap peers | "" |
| `-backup_filename` | Backup file location | "" |
| `-shard_cap` | Maximum dats per shard | 10000 |
| `-log_level` | Logging verbosity (ERROR/DEBUG) | "ERROR" |

## Commands

**Key Generation**
```bash
dave keygen [filename]
```

**Store Data**
```bash
dave put <key> <value>
```

## Storage Architecture

- Sharded storage with configurable capacity
- Trust-based peer selection
- Proof-of-work prioritization
- Automatic data pruning
- Backup and recovery

## Network Protocol

- UDP-based transport
- Protobuf serialization
- Maximum packet size: 1424 bytes
- Random push distribution model
- Trust-based peer selection

## Security Features

- Ed25519 signatures
- Proof-of-work spam prevention
- Trust-based resource allocation
- Challenge-response peer verification

## Performance Considerations

- Concurrent shard processing
- Configurable pruning intervals
- Ring buffer for recent data
- Trust-based resource allocation
