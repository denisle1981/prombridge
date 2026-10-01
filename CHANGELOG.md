# Changelog

## 2.3.0

- Added `--metric-regex` metric-family filtering after `--job-regex`.
- Added `--max-bandwidth-bytes-per-second`; default 4096 B/s.
- UDP datagrams are paced through the configured bandwidth ceiling, including full synchronization batches.
- Added Agent bandwidth Gauge and configured-limit Gauge.
- Added Gateway receive-bandwidth Gauge and UDP wire-byte counter.
- Added Windows monitoring allow-list example covering availability, CPU, memory, disk, network utilization, and RX/TX error-rate source metrics.
- Updated Grafana dashboard for a 4 KiB/s PromBridge allocation and 10 KiB/s physical-link reference.
- Removed dashboard dependency on transported `instance` labels.

# Changelog

## v2.2.0

### Added
- `--startup-mode=full|current`.
- `--job-regex=<regex>` for filtering by Prometheus `job` label.
- `--send-mode=delta|snapshot`.
- Delta state tracker that transmits only changed/new/deleted series.
- No-change snapshot suppression.
- Sender metrics separating observed and transmitted sample counts.
- Last-batch raw/encoded/compressed byte gauges.
- Gateway `--preserve-source-timestamps` flag; default false.

### Changed
- Periodic full reconciliation in `startup-mode=current` includes only series that have been transmitted since startup, so baseline-only series are not resurrected.
- Sequence numbers now advance only when a UDP batch is actually transmitted.

### Explanation of previous 5s/15s behavior
- v2.1.0 transmitted a current snapshot of all series each interval.
- `/federate` is snapshot-based, not an interval-history API.
- Therefore per-batch size was expected to remain roughly constant when changing only the interval.

## v2.3.1 - stress/reassembly fix

- Fixed receiver reassembly timeout semantics: `--reassembly-timeout` is now an idle/progress timeout instead of a maximum total message duration. Bandwidth-shaped multi-packet messages may therefore take longer than the configured timeout to complete as long as chunks continue arriving.
- Added regression coverage for transfers whose total duration exceeds the reassembly timeout while chunk progress continues.
- Corrected Grafana bandwidth panels to use the sender's measured wire rate and configured shaping limit instead of deriving an apparent rate from completed compressed batches over a Prometheus window.
