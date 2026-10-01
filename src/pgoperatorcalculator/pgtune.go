package pgoperatorcalculator

import (
	"math"
	"strconv"
)

// PgTuneInput mirrors the PgTune form with the OS fixed to Linux.
// CPU and Connections are optional: zero means "not given".
type PgTuneInput struct {
	Version     int
	DBType      string
	RAMKB       int64
	CPU         int
	Connections int
	HDType      string
	DBSize      string
}

// PgTuneResult holds the emitted parameters in PgTune order, the warnings
// PgTune prepends to the generated config, and the raw kB values the
// capacity estimate needs.
type PgTuneResult struct {
	Parameters        []Parameter
	Warnings          []string
	MaxConnections    int
	Workers           int
	SharedBuffersKB   int64
	WorkMemKB         int64
	MaintenanceWorkKB int64
	WalBuffersKB      int64
}

// PgTune ports the calculations documented in pgtune/parameter_calc.md.
func PgTune(in PgTuneInput) PgTuneResult {
	var r PgTuneResult
	add := func(name, value string) { r.Parameters = append(r.Parameters, Parameter{name, value}) }
	ram := in.RAMKB
	desktop := in.DBType == DbTypeDesktop

	if ram < 256*mB {
		r.Warnings = append(r.Warnings, "this tool not being optimal for low memory systems")
	}
	if ram > 100*gB {
		r.Warnings = append(r.Warnings, "this tool not being optimal for very high memory systems")
	}

	r.MaxConnections = in.Connections
	if r.MaxConnections == 0 {
		r.MaxConnections = map[string]int{DbTypeWeb: 200, DbTypeOLTP: 300, DbTypeDW: 40, DbTypeDesktop: 20, DbTypeMixed: 100}[in.DBType]
	}
	add("max_connections", strconv.Itoa(r.MaxConnections))

	r.SharedBuffersKB = ram / 4
	if desktop {
		r.SharedBuffersKB = ram / 16
	}
	add("shared_buffers", FormatKB(r.SharedBuffersKB))

	ecs := ram * 3 / 4
	if desktop {
		ecs = ram / 4
	}
	add("effective_cache_size", FormatKB(ecs))

	r.MaintenanceWorkKB = ram / 16
	if in.DBType == DbTypeDW {
		r.MaintenanceWorkKB = ram / 8
	}
	if r.MaintenanceWorkKB >= 8*gB {
		r.MaintenanceWorkKB = 8 * gB
	}
	add("maintenance_work_mem", FormatKB(r.MaintenanceWorkKB))

	add("checkpoint_completion_target", "0.9")

	wb := 3 * r.SharedBuffersKB / 100
	if wb > 16*mB {
		wb = 16 * mB
	}
	if wb > 14*mB && wb < 16*mB {
		wb = 16 * mB
	}
	if wb < 32 {
		wb = 32
	}
	r.WalBuffersKB = wb
	add("wal_buffers", FormatKB(wb))

	if in.DBType == DbTypeDW {
		add("default_statistics_target", "500")
	} else {
		add("default_statistics_target", "100")
	}

	switch {
	case in.DBSize == DBSizeLessRAM:
		add("random_page_cost", "1.1")
	case in.HDType == HDTypeHDD:
		add("random_page_cost", "4")
	case in.DBType == DbTypeDW:
		add("random_page_cost", "4")
		r.Warnings = append(r.Warnings, "random_page_cost was left at 4 for dw on non-HDD storage to avoid index scans on large analytical reads")
	default:
		add("random_page_cost", "1.1")
	}

	add("effective_io_concurrency", map[string]string{HDTypeHDD: "2", HDTypeSSD: "200", HDTypeSAN: "300", HDTypeNVMe: "1000"}[in.HDType])

	r.Workers = 8
	if in.CPU >= 4 {
		r.Workers = in.CPU
	}
	base := float64(ram-r.SharedBuffersKB) / float64((r.MaxConnections+r.Workers)*3)
	var wm int64
	switch in.DBType {
	case DbTypeWeb, DbTypeOLTP:
		wm = int64(math.Floor(base))
	case DbTypeDW, DbTypeMixed:
		wm = int64(math.Floor(base / 2))
	case DbTypeDesktop:
		wm = int64(math.Floor(base / 6))
	}
	switch in.DBSize {
	case DBSizeLessRAM:
		wm = int64(math.Floor(float64(wm) * 1.3))
	case DBSizeGreaterRAM:
		wm = int64(math.Floor(float64(wm) * 0.9))
	}
	if wm < 4*mB {
		wm = 4 * mB
	}
	r.WorkMemKB = wm
	add("work_mem", FormatKB(wm))

	if r.SharedBuffersKB >= 2*gB {
		add("huge_pages", "try")
	} else {
		add("huge_pages", "off")
	}

	if in.Version >= 12 && (in.DBType == DbTypeWeb || in.DBType == DbTypeOLTP || in.DBType == DbTypeMixed) {
		add("jit", "off")
	}

	if in.Version >= 15 {
		add("wal_compression", "lz4")
		r.Warnings = append(r.Warnings, "wal_compression = lz4 requires PostgreSQL built --with-lz4")
	} else {
		add("wal_compression", "on")
	}

	if in.CPU >= 32 {
		add("autovacuum_max_workers", "5")
	} else if in.CPU >= 16 {
		add("autovacuum_max_workers", "4")
	}

	if r.MaintenanceWorkKB >= 2*gB {
		add("autovacuum_work_mem", FormatKB(2*gB))
	}

	// io_workers is never emitted on Linux because io_method is io_uring.
	if in.Version >= 18 {
		add("io_method", "io_uring")
		r.Warnings = append(r.Warnings, "io_method = io_uring requires PostgreSQL built --with-liburing and a container runtime/seccomp profile that permits io_uring")
	}

	walSizes := map[string][2]int64{
		DbTypeWeb:     {1 * gB, 4 * gB},
		DbTypeOLTP:    {2 * gB, 8 * gB},
		DbTypeDW:      {4 * gB, 16 * gB},
		DbTypeDesktop: {100 * mB, 2 * gB},
		DbTypeMixed:   {1 * gB, 4 * gB},
	}[in.DBType]
	add("min_wal_size", FormatKB(walSizes[0]))
	add("max_wal_size", FormatKB(walSizes[1]))

	if in.CPU >= 4 {
		half := (in.CPU + 1) / 2
		gather := half
		if in.DBType != DbTypeDW && gather > 4 {
			gather = 4
		}
		add("max_worker_processes", strconv.Itoa(in.CPU))
		add("max_parallel_workers_per_gather", strconv.Itoa(gather))
		add("max_parallel_workers", strconv.Itoa(in.CPU))
		if in.Version >= 11 {
			add("max_parallel_maintenance_workers", strconv.Itoa(min(half, 4)))
		}
	}

	if desktop {
		add("wal_level", "minimal")
		add("max_wal_senders", "0")
		r.Warnings = append(r.Warnings, "wal_level = minimal disables streaming replication and WAL archiving; Patroni replicas and pgBackRest will not work")
	}
	return r
}
