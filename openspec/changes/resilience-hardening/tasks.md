## 1. Bounded operational logging

- [x] 1.1 Add safe rotating file writer with restrictive permissions and bounded history; verify threshold, reopen, close, nonregular-path and concurrent-write tests.
- [x] 1.2 Wire lifecycle/provider/transfer/verification/storage failure diagnostics without sensitive remote bodies or private public errors; verify targeted failure tests and log assertions.

## 2. Failure boundaries and review

- [x] 2.1 Audit requested offline, catalog, disk, interruption, checksum, runtime, port and SQLite failures plus filesystem/process boundaries; fix concrete gaps and verify associated regressions.
- [x] 2.2 Deliver failure-mode matrix with implementation/test evidence, run relevant Go/frontend checks, and independently review all resilience scenarios before marking complete.
