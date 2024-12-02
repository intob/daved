# Dave CLI

Dave is a distributed key-value store built on UDP. XOR distance metric is used to select storage replicas. Random storage challenges measure peer reliability.

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
| `-log_unbuffered` | Set to any value to write to stdout without buffer | "" |

## Commands

**Key Generation**
```bash
dave keygen [filename]
```

**Store Data**
```bash
dave put <key> <value>
```
