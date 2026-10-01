package pgoperatorcalculator

const (
	VERSION = "v0.1.0"

	OkI            = 1001
	ClosetolimitI  = 2001
	OverutilizingI = 3001
	ErrorexecI     = 5001

	OkT            = "Execution was successful and resources match the requested workload"
	ClosetolimitT  = "Execution was successful however resources are close to saturation"
	OverutilizingT = "Resources are not enough to cover the requested workload"
	ErrorexecT     = "There is an error while processing. See details: %s"

	DbTypeWeb     = "web"
	DbTypeOLTP    = "oltp"
	DbTypeDW      = "dw"
	DbTypeDesktop = "desktop"
	DbTypeMixed   = "mixed"

	HDTypeHDD  = "hdd"
	HDTypeSSD  = "ssd"
	HDTypeSAN  = "san"
	HDTypeNVMe = "nvme"

	DBSizeLessRAM    = "less_ram"
	DBSizeMidRAM     = "mid_ram"
	DBSizeGreaterRAM = "greater_ram"

	MemoryUnitMB = "MB"
	MemoryUnitGB = "GB"
	MemoryUnitTB = "TB"

	FamilyTypePostgres  = "postgres"
	FamilyTypeMonitor   = "monitor"
	FamilyTypeBackup    = "backup"
	FamilyTypePgBouncer = "pgbouncer"

	GroupNameConfiguration = "configuration"
	GroupNameResources     = "resources"

	ResultOutputFormatJson  = "json"
	ResultOutputFormatHuman = "human"

	MinPGVersion   = 10
	MaxPGVersion   = 18
	MinConnections = 20

	// All PgTune memory math is done in kB.
	kB = int64(1)
	mB = 1024 * kB
	gB = 1024 * mB
	tB = 1024 * gB

	MinMemoryKB = 512 * mB

	CloseLimitPct = 0.85
	// Per-backend private memory (catalog caches, plan caches, stack) used by
	// the capacity estimate. PgTune itself does not model this.
	ConnectionOverheadKB = 2 * mB
	// The smallest CPU allotment accepted for the postgres container.
	MinPostgresCPU = 500

	// Instance pod sidecars, in millicores and kB. Request equals limit so
	// the instance pod gets the Guaranteed QoS class. These are deducted
	// from the pod before PgTune sizes the postgres container.
	PMMCPU      = 400
	PMMMemoryKB = 256 * mB

	// pgBackRest compresses on the PostgreSQL side before sending, and each
	// of its process-max processes compresses single-threaded, so it can
	// keep one core busy. Memory covers each process's I/O buffers and
	// compression stream state on top of the main process and TLS server.
	DefaultPgBackRestProcessMax = 4
	MaxPgBackRestProcessMax     = 32
	PgBackRestBaseCPU           = 200
	PgBackRestProcessCPU        = 1000
	PgBackRestBaseMemKB         = 128 * mB
	PgBackRestProcessMemKB      = 64 * mB
	// Guaranteed QoS holds the backup CPU even when no backup runs, so it
	// is capped at this share of the pod; a capped sidecar only makes
	// backups slower. Memory is never capped because exceeding it kills
	// the backup.
	PgBackRestMaxCPUPct = 0.25
	PgBackRestMinCPU    = 500
	// pgBouncer runs in its own pods, so its resources are not deducted.
	PgBouncerRequestCPU   = 250
	PgBouncerLimitCPU     = 1000
	PgBouncerRequestMemKB = 128 * mB
	PgBouncerLimitMemKB   = 256 * mB
)
