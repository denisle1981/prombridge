# Benchmark plan

Use a real `/federate` snapshot from the Windows Prometheus and run at least 1,000 transfer cycles.

Collect from sender logs/self-metrics:

- raw federation bytes;
- binary encoded bytes;
- compressed bytes;
- parsing, serialization, compression and UDP send durations;
- packet count and sender CPU/RSS.

Collect from receiver logs/self-metrics:

- reassembly, decompression, deserialization and publish durations;
- unknown Series IDs and sequence gaps;
- receiver CPU/RSS and UDP drops (`netstat -s` / node metrics).

Report p50, p95, p99 and maximum for every duration, plus average and peak CPU/memory.

Built-in Go microbenchmark:

```bash
go test -bench=. -benchmem ./internal/metriccodec
```

Compare production runs with:

```text
--compression=none
--compression=gzip
```

The next compression decision should be based on the actual 9,000+ series payload rather than generic compressor benchmarks.
