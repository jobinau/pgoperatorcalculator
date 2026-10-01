# PostgreSQL Operator Calculator

The PostgreSQL Operator Calculator is a Go library and HTTP service that produces `postgresql.conf` and Kubernetes resource recommendations for PostgreSQL clusters managed by the Percona Operator for PostgreSQL.

It mirrors the public API shape of the [MySQL Operator Calculator](https://github.com/Tusamarco/mysqloperatorcalculator) and the MongoDB Operator Calculator. The PostgreSQL parameters are a faithful port of [PgTune](https://pgtune.leopard.in.ua/) for Linux. Kubernetes awareness is added on top: sidecar resources are removed from the pod before PgTune sizes the `postgres` container.

## Scope

- PostgreSQL 10 to 18. The Percona Operator itself supports a narrower range; check the operator release notes.
- PgTune inputs: workload type, memory, CPU count, connections, storage type and data size relative to RAM.
- Linux only. Operator pods always run Linux, so PgTune's Windows and macOS branches, and `io_workers`, are not implemented.
- One `postgres` instance pod per result: the `database` container plus optional PMM and pgBackRest sidecars.
- Optional pgBouncer pod resources.
- Kubernetes CPU and memory requests and limits.
- JSON and human-readable output. In the human output, the `[postgres.configuration]` section is valid `postgresql.conf` syntax.
- No storage sizing, replica count, Patroni tuning, backup scheduling, security policy, probes or autoscaling.

## Build and Run

```bash
git clone https://github.com/jobinau/pgoperatorcalculator.git
cd pgoperatorcalculator
go build -o pgoperatorcalculator ./src
./pgoperatorcalculator
```

The default server address is `0.0.0.0:8080`.

## Command-Line Flags

| Flag | Default | Description |
|---|---:|---|
| `-address` | `0.0.0.0` | IP address or hostname to bind to. |
| `-port` | `8080` | TCP port to listen on. |
| `-loglevel` | `INFO` | Log level: `ERROR`, `INFO`, or `DEBUG`. Unknown values fall back to `INFO`. |
| `-version` | `false` | Print the calculator version and exit. |
| `-help` | `false` | Print command-line usage and exit. |

## Input Parameters

All inputs are passed as a JSON request to `POST /calculator` or through the Go module API.

| Parameter | Type | Required | Description |
|---|---|---:|---|
| `output` | string | Yes | `json` for structured output or `human` for text. Any value other than `human` uses JSON. |
| `pgversion.major` | integer | Yes | PostgreSQL major version, `10` to `18`. |
| `pgversion.minor` | integer | No | Echoed only. |
| `pgversion.patch` | integer | No | Echoed only. |
| `dbtype` | string | Yes | Workload type: `web`, `oltp`, `dw`, `desktop` or `mixed`. |
| `totalmemory` | integer | Yes | Total memory of the instance pod, in `totalmemoryunit`. The pod total must be at least 512MB. |
| `totalmemoryunit` | string | Yes | `MB`, `GB` or `TB` (binary: 1GB = 1024MB). |
| `cpunum` | integer | No | Total CPU cores of the instance pod. `0` or omitted means not given. If omitted, the parallel settings and CPU requests/limits are left out, and the pod cannot be `Guaranteed` QoS. |
| `connections` | integer | No | `max_connections`. `0` or omitted uses the workload default. When given it must be at least `20`. |
| `hdtype` | string | Yes | Data storage: `hdd`, `ssd`, `san` or `nvme`. |
| `dbsize` | string | Yes | Data size relative to the `postgres` container's RAM: `less_ram` (< RAM), `mid_ram` (1x to 3x RAM) or `greater_ram` (> 3x RAM). |
| `providercostpct` | float | No | Fraction of the pod reserved for platform overhead, e.g. `0.12`. Must be ≥ 0 and < 1. Default `0`. |
| `pgdedicated` | boolean | No | When `true`, leave out the PMM sidecar and give its share to `postgres`. Default `false`. |
| `backup.enabled` | boolean | No | When `true`, add the pgBackRest sidecar to the instance pod. Default `false`. |
| `backup.processmax` | integer | No | pgBackRest `process-max`: parallel compression/transfer processes, `1` to `32`. Default `4`. Sets the sidecar's size (see below). |
| `pgbouncer.enabled` | boolean | No | When `true`, also emit resources for a pgBouncer pod. Default `false`. |

String values are case-insensitive and trimmed. Invalid requests return message type `5001` and HTTP status `400`.

### Workload types

| `dbtype` | Description | Default `max_connections` |
|---|---|---:|
| `web` | Web application | 200 |
| `oltp` | Online transaction processing | 300 |
| `dw` | Data warehouse | 40 |
| `desktop` | Desktop application | 20 |
| `mixed` | Mixed type of application | 100 |

## Resource Allocation

The memory and CPU you request describe the whole instance pod. Before PgTune runs, the calculator:

1. Removes `providercostpct` from both memory and CPU.
2. Removes the PMM sidecar limits (`400m`, `256Mi`) unless `pgdedicated` is true.
3. Removes the pgBackRest sidecar (sized from `backup.processmax`, see [pgBackRest Sizing](#pgbackrest-sizing)) when `backup.enabled` is true.

The remainder is the `postgres` container. PgTune uses that memory as `RAM`. For PgTune's `cpu`, the container's millicores are rounded **up** to whole cores, because the parallel settings count worker processes rather than CPU time. For example, a 4-core pod with PMM leaves `3600m`, and PgTune is given `cpu = 4`. The `incoming.allocation` block in the response echoes the split.

If the container is left with less than 512MB of memory or less than `500m` CPU, the request returns `3001` (resources not enough).

Fixed sidecar and pgBouncer resources:

| Component | Request CPU | Limit CPU | Request memory | Limit memory | Deducted from pod |
|---|---:|---:|---:|---:|:---:|
| PMM client | `400m` | `400m` | `256Mi` | `256Mi` | Yes |
| pgBackRest | see below | = request | see below | = request | Yes |
| pgBouncer | `250m` | `1000m` | `128Mi` | `256Mi` | No (separate pod) |

### pgBackRest Sizing

pgBackRest compresses each file on the PostgreSQL side before sending it to the repository. It runs `process-max` processes in parallel, and each one compresses single-threaded. With the default `gz` compression at level 6, one process can keep a full core busy. So the sidecar is sized per process:

```
memory = 128Mi + processmax × 64Mi                  (never capped)
cpu    = min(200m + processmax × 1000m,
             max(500m, 25% of pod CPU after providercostpct))
```

| Pod CPU | `processmax` | pgBackRest CPU | pgBackRest memory |
|---:|---:|---:|---:|
| 4 | 4 | `1000m` (capped) | `384Mi` |
| 8 | 4 | `2000m` (capped) | `384Mi` |
| 32 | 4 | `4200m` | `384Mi` |
| 32 | 8 | `8000m` (capped) | `640Mi` |

Why CPU is capped but memory isn't: with Guaranteed QoS the reservation is held all the time, including between backups. Without a cap, a 4-core pod would give most of its CPU to an idle sidecar. A capped sidecar only throttles compression, so backups take longer. Running out of memory would OOM-kill the backup, so memory always covers every process. When the cap applies, the response includes a warning.

The CPU estimate assumes `gz` or `zstd`-class compression. `compress-type=lz4` needs much less CPU per process, and `bz2` needs more. `process-max` is emitted in `pgbackrest.configuration` so pgBackRest uses the parallelism the sidecar was sized for.

Removing the sidecar from the pod also changes what PgTune is given. On a 4-core pod, PMM (`400m`) plus pgBackRest (`1000m`) leaves `2600m`. That rounds up to `cpu = 3`, which is below PgTune's threshold of 4, so the parallel settings are omitted.

### Guaranteed QoS

Every container the calculator sizes in the instance pod (`postgres`, PMM, pgBackRest) has **request = limit** for both CPU and memory. That puts the pod in the Kubernetes `Guaranteed` QoS class. As a result:

- When a node runs short of memory, the database pod is evicted after `Burstable` and `BestEffort` pods.
- The pod gets the lowest OOM score.
- With the kubelet's `static` CPU manager policy and whole-core CPU values, it can be given exclusive cores.

PostgreSQL reserves most of its memory up front (`shared_buffers`, `wal_buffers`) and keeps a steady working set, so a request below the limit would only make the scheduler under-count the pod's real footprint.

Things to check:

- **`cpunum` is required for Guaranteed.** Without it no CPU values can be emitted, so the pod is `Burstable`, and the response includes a warning saying so.
- **Operator-added containers count too.** The pod is only `Guaranteed` if *every* container, including init containers, has request = limit. Containers the operator adds that this calculator does not size (for example `replication-cert-copy` or `pgbackrest-config`) must also be given equal requests and limits in the custom resource.
- **pgBouncer is not affected.** It runs in its own stateless pods, so its request is still below its limit.

## PostgreSQL Parameters

The rules below follow PgTune for Linux. All memory math is in kB. A value is shown as `GB` if it divides evenly, otherwise `MB` if it divides evenly, otherwise `kB`. Parameters marked "omitted" are left out so PostgreSQL's default applies. `RAM` means the `postgres` container memory.

| Parameter | Rule |
|---|---|
| `max_connections` | `connections`, else the workload default. |
| `shared_buffers` | RAM / 4; desktop RAM / 16. |
| `effective_cache_size` | RAM × 3/4; desktop RAM / 4. |
| `maintenance_work_mem` | RAM / 16; dw RAM / 8; capped at 8GB. |
| `checkpoint_completion_target` | `0.9`. |
| `wal_buffers` | 3% of `shared_buffers`, capped at 16MB; values between 14MB and 16MB round up to 16MB; minimum 32kB. |
| `default_statistics_target` | dw `500`, otherwise `100`. |
| `random_page_cost` | `less_ram` → `1.1`; `hdd` → `4`; dw → `4` (with warning); otherwise `1.1`. |
| `effective_io_concurrency` | hdd `2`, ssd `200`, san `300`, nvme `1000`. |
| `work_mem` | `(RAM − shared_buffers) / ((max_connections + workers) × 3)`, where workers = `cpu` if ≥ 4 else 8; then ÷2 for dw/mixed and ÷6 for desktop; +30% for `less_ram`, −10% for `greater_ram`; minimum 4MB. |
| `huge_pages` | `try` when `shared_buffers` ≥ 2GB, otherwise `off`. |
| `jit` | `off` for PG ≥ 12 and web/oltp/mixed, otherwise omitted. |
| `wal_compression` | PG ≥ 15 `lz4` (with warning), otherwise `on`. |
| `autovacuum_max_workers` | cpu ≥ 32 → `5`; cpu ≥ 16 → `4`; otherwise omitted. |
| `autovacuum_work_mem` | `2GB` when `maintenance_work_mem` ≥ 2GB, otherwise omitted. |
| `io_method` | PG ≥ 18 `io_uring` (with warning), otherwise omitted. |
| `min_wal_size` / `max_wal_size` | web 1GB/4GB, oltp 2GB/8GB, dw 4GB/16GB, desktop 100MB/2GB, mixed 1GB/4GB. |
| `max_worker_processes` | `cpu`, only when cpu ≥ 4. |
| `max_parallel_workers_per_gather` | `ceil(cpu / 2)`, capped at 4 unless dw; only when cpu ≥ 4. |
| `max_parallel_workers` | `cpu`, only when cpu ≥ 4. |
| `max_parallel_maintenance_workers` | `min(ceil(cpu / 2), 4)` for PG ≥ 11, only when cpu ≥ 4. |
| `wal_level` / `max_wal_senders` | desktop only: `minimal` / `0` (with warning). |

The port has been checked against the PgTune JavaScript selectors over 20,000 random Linux inputs. Every parameter value and the parameter order matched.

### Warnings

`message.warnings` lists any of these that apply:

- Memory is below 256MB or above 100GB, where PgTune is less accurate.
- `wal_compression = lz4` needs PostgreSQL built `--with-lz4`.
- `io_method = io_uring` needs PostgreSQL built `--with-liburing`, and a container runtime/seccomp profile that allows io_uring. Many default container seccomp profiles block it.
- dw on non-HDD storage keeps `random_page_cost = 4`.
- The pgBackRest CPU was capped, so backups will be throttled.
- `cpunum` was not given, so the pod cannot be `Guaranteed` QoS.
- desktop's `wal_level = minimal` turns off streaming replication and WAL archiving, so Patroni replicas and pgBackRest will not work.

## Capacity Estimate

PgTune always produces values. On top of that, the calculator estimates steady-state memory use of the `postgres` container:

```
shared_buffers + wal_buffers + maintenance_work_mem
  + (max_connections + workers) × (work_mem + 2MB per-backend overhead)
```

| Utilization | Message type | HTTP status |
|---|---|---|
| ≤ 85% | `1001` OK | 200 |
| > 85% | `2001` close to limit | 200 |
| > 100% | `3001` overloaded, empty `answer` | 422 |

The 4MB `work_mem` floor is what usually causes overload: many connections on a small pod. Lower `connections` or raise `totalmemory`.

## Output Structure

The JSON response has three sections:

- `message`: `type`, `name`, `text` and an optional `warnings` list.
- `incoming`: the normalized request plus `allocation`, which shows the pod, `postgres` container and PgTune CPU split.
- `answer`: configuration families.

| Family | Groups | Present |
|---|---|---|
| `postgres` | `configuration`, `resources` | Always |
| `monitor` | `resources` (PMM client) | Unless `pgdedicated` |
| `backup` | `configuration` (`process-max`), `resources` (pgBackRest sidecar) | When `backup.enabled` |
| `pgbouncer` | `resources` | When `pgbouncer.enabled` |

Resource values: CPU in millicores (`"4000m"`), memory in bytes. Instance pod containers always have request = limit.

## HTTP API

### `GET /supported`

Returns the accepted values for each input:

```bash
curl -s http://127.0.0.1:8080/supported
```

### `POST /calculator`

```bash
curl -s -X POST -H "Content-Type: application/json" -d '{
  "output": "human",
  "pgversion": {"major": 17, "minor": 6, "patch": 0},
  "dbtype": "oltp",
  "totalmemory": 32,
  "totalmemoryunit": "GB",
  "cpunum": 8,
  "connections": 500,
  "hdtype": "ssd",
  "dbsize": "mid_ram",
  "backup": {"enabled": true},
  "pgbouncer": {"enabled": true}
}' http://127.0.0.1:8080/calculator
```

```ini
[message]
name = Execution was successful and resources match the requested workload
type = 1001
text = estimated memory utilization is 59%
# WARNING
# - wal_compression = lz4 requires PostgreSQL built --with-lz4
# - pgBackRest CPU is capped at 2000m (25% of the pod); its 4 parallel compression processes will be throttled and backups will take longer
[pgbackrest.configuration]
process-max = 4
[pgbackrest.resources]
limit_cpu = 2000m
limit_memory = 402653184
request_cpu = 2000m
request_memory = 402653184
[pmm-client.resources]
limit_cpu = 400m
limit_memory = 268435456
request_cpu = 400m
request_memory = 268435456
[pgbouncer.resources]
limit_cpu = 1000m
limit_memory = 268435456
request_cpu = 250m
request_memory = 134217728
[postgres.configuration]
max_connections = 500
shared_buffers = 8032MB
effective_cache_size = 24096MB
maintenance_work_mem = 2008MB
checkpoint_completion_target = 0.9
wal_buffers = 16MB
default_statistics_target = 100
random_page_cost = 1.1
effective_io_concurrency = 200
work_mem = 16254kB
huge_pages = try
jit = off
wal_compression = lz4
min_wal_size = 2GB
max_wal_size = 8GB
max_worker_processes = 6
max_parallel_workers_per_gather = 3
max_parallel_workers = 6
max_parallel_maintenance_workers = 3
[postgres.resources]
limit_cpu = 5600m
limit_memory = 33688649728
request_cpu = 5600m
request_memory = 33688649728
```

## Using as a Go Module

```go
import PG "github.com/jobinau/pgoperatorcalculator/src/pgoperatorcalculator"

var conf PG.Configuration
conf.Init()

request := PG.ConfigurationRequest{
	Output:          PG.ResultOutputFormatJson,
	PGVersion:       PG.Version{Major: 17},
	DBType:          PG.DbTypeOLTP,
	TotalMemory:     32,
	TotalMemoryUnit: PG.MemoryUnitGB,
	CPUNum:          8,
	HDType:          PG.HDTypeSSD,
	DBSize:          PG.DBSizeMidRAM,
}

var calculator PG.PGOperatorCalculator
calculator.Init(request, conf)
err, message, families := calculator.GetCalculate()
if err != nil {
	// message.MType is 5001 (invalid) or 3001 (overloaded)
}
output, _ := calculator.GetJSONOutput(message, calculator.IncomingRequest, families)
```

`calculator.IncomingRequest` holds the normalized request with its `Allocation` after `GetCalculate()`. You can also call `PG.PgTune(PG.PgTuneInput{...})` directly to get PgTune's output without the Kubernetes layer. See `src/example/example.go`.

## Percona Operator Mapping

| Calculator output | Percona Operator for PostgreSQL target |
|---|---|
| `postgres.configuration` | `spec.patroni.dynamicConfiguration.postgresql.parameters` |
| `postgres.resources` | `spec.instances[].resources` |
| `monitor.resources` | `spec.pmm.resources` |
| `backup.configuration` | `spec.backups.pgbackrest.global` (e.g. `process-max: "4"`) |
| `backup.resources` | `spec.backups.pgbackrest.sidecars.pgbackrest.resources` |
| `pgbouncer.resources` | `spec.proxy.pgBouncer.resources` |

`huge_pages = try` only takes effect if the pod also requests `hugepages-2Mi` (or `hugepages-1Gi`) resources. Otherwise PostgreSQL falls back to regular pages.

## Verification

```bash
gofmt -l src
go vet ./...
go test ./...
go build -o /tmp/pgoperatorcalculator ./src
```
