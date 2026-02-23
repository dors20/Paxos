# Paxos

A Go implementation of a **Stable-Leader Paxos** variant with a distributed banking application on top.

## Overview

This project implements a sequence of Paxos instances to achieve consensus with **2f + 1** nodes (f = max concurrent faulty nodes). It provides:

- **Consensus**: Basic Paxos–style prepare/promise, accept/accepted, and commit phases.
- **Stable-Leader Paxos**: One leader handles multiple transactions under a single ballot; leader election only when the leader is suspected faulty.
- **Banking application**: Clients submit transfer transactions **(sender, receiver, amount)**; state machine replication keeps balances consistent across replicas.

## Protocol Summary

### Leader election
- Proposer sends `⟨PREPARE, b⟩`; nodes reply `⟨ACK, b, AcceptLog⟩` with their accepted (ballot, seq, value) log.
- Proposer becomes leader after **f + 1** promises, then sends `⟨NEW-VIEW, b, AcceptLog⟩` (with no-ops for gaps).

### Normal operation
- Leader broadcasts `⟨ACCEPT, b, s, m⟩` (ballot, sequence number, client request).
- Backups accept and reply `⟨ACCEPTED, b, s, m, nb⟩`.
- After **f + 1** accepted, leader broadcasts `⟨COMMIT, b, s, m⟩`; all nodes execute in sequence order and leader sends `⟨REPLY, b, τ, c, r⟩` to the client.

### Failure handling
- Backup failure: system continues if ≤ f nodes are faulty.
- Leader failure: backup timers expire; new leader election and NEW-VIEW recovery so committed and in-flight transactions are preserved.

## Implementation details

- **5 nodes** (2f + 1, f = 2), **10 clients**.
- Nodes are separate processes; communication via **gRPC** (see `api/api.proto`).
- Clients start with 10 units; transactions are **(s, r, amt)**. Exactly-once semantics via client timestamps.
- **PrintLog**, **PrintDB**, **PrintStatus**, **PrintView** for inspection after each test set.

## Building and running

Generate protobuf/grpc code:

```bash
protoc --go_out=. --go-grpc_out=. api/api.proto
```

Run servers and client according to your project’s run scripts or instructions in `runs/`.

## Test format

Tests are defined in CSV files with three columns:

| Column        | Description |
|---------------|-------------|
| Set Number    | Test case id. |
| Transactions  | One per row: (Sender, Receiver, Amount). |
| Live Nodes    | Active nodes for that set, e.g. `[n1, n2, n3, n4, n5]`. |

`LF` in the Live Nodes column indicates a leader failure before the next set. Sets are executed in order; the program waits for user input before processing the next set.

## Robust test cases (subset)

The file `CSE535-F25-Project-1-Testcases.csv` includes a subset of robust test cases. Summary:

| Set | Transactions (sample) | Live nodes |
|-----|------------------------|------------|
| 1   | (A,J,3), (C,D,1), (B,E,4), (F,G,2), (H,I,2) | [n1,n2,n3,n4,n5] |
| 2   | (B,I,2), (J,C,4) | [n1,n2,n3,n4] |
| 3   | (B,F,3), (C,A,2) | [n1,n3,n5] |
| 4   | (B,F,3), (A,H,3) | [n1,n2,n3,n5] |
| 5   | (C,B,1), (D,F,2) | [n1,n2] |
| 6   | (A,B,4), (I,D,2), (E,J,2), LF | [n1..n5] |
| 7   | (F,A,2), (G,H,1), (J,A,2) | [n1..n5] |
| 8   | (E,A,3), (I,B,2), (C,D,2), (G,H,2), LF, (F,A,2), (J,B,1) | [n1,n2,n3] |
| 9   | (C,H,3), LF, (E,D,1), (G,I,2), LF, (A,J,1) | [n1..n5] |
| 10  | Long sequence of (A,B,1), (D,E,1), (I,J,1), … (20+ transactions) | [n1..n5] |

These cover: startup leader election, full and partial node sets, backup and leader failures (LF), and a long run for stability.

## References

- Leslie Lamport, *Paxos made simple*, ACM SIGACT News, 32(4):18–25, 2001.

## License

Academic project; see course and university policies on use and attribution.
