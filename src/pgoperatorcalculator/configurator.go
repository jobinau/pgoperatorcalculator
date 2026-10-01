package pgoperatorcalculator

import (
	"fmt"
	"math"
	"strconv"
)

type Configurator struct {
	request  ConfigurationRequest
	conf     Configuration
	families map[string]Family
}

func invalid(text string) (ResponseMessage, map[string]Family, error) {
	return ResponseMessage{MType: ErrorexecI, MName: "Invalid request", MText: text}, nil, fmt.Errorf("%s", text)
}
func overloaded(text string) (ResponseMessage, map[string]Family, error) {
	return ResponseMessage{MType: OverutilizingI, MName: "Resources overloaded", MText: text}, nil, fmt.Errorf("%s", text)
}

func (c *Configurator) validate() error {
	r := c.request
	if r.PGVersion.Major < MinPGVersion || r.PGVersion.Major > MaxPGVersion {
		return fmt.Errorf("only PostgreSQL %d to %d are supported", MinPGVersion, MaxPGVersion)
	}
	if _, ok := c.conf.GetDBType(r.DBType); !ok {
		return fmt.Errorf("unsupported dbtype %q", r.DBType)
	}
	if r.TotalMemory <= 0 {
		return fmt.Errorf("totalmemory must be greater than zero")
	}
	unit, err := MemoryUnitKB(r.TotalMemoryUnit)
	if err != nil {
		return err
	}
	if r.TotalMemory*unit < MinMemoryKB {
		return fmt.Errorf("totalmemory must be at least 512MB")
	}
	if r.CPUNum < 0 {
		return fmt.Errorf("cpunum must be at least 1 when given")
	}
	if r.Connections != 0 && r.Connections < MinConnections {
		return fmt.Errorf("connections must be at least %d when given", MinConnections)
	}
	switch r.HDType {
	case HDTypeHDD, HDTypeSSD, HDTypeSAN, HDTypeNVMe:
	default:
		return fmt.Errorf("unsupported hdtype %q", r.HDType)
	}
	switch r.DBSize {
	case DBSizeLessRAM, DBSizeMidRAM, DBSizeGreaterRAM:
	default:
		return fmt.Errorf("unsupported dbsize %q", r.DBSize)
	}
	if r.Backup.ProcessMax < 0 || r.Backup.ProcessMax > MaxPgBackRestProcessMax {
		return fmt.Errorf("backup.processmax must be between 1 and %d when given", MaxPgBackRestProcessMax)
	}
	if r.ProviderCostPct < 0 || r.ProviderCostPct >= 1 {
		return fmt.Errorf("providercostpct must be greater than or equal to 0 and less than 1")
	}
	return nil
}

func (c *Configurator) calculate() (ResponseMessage, map[string]Family, error) {
	if err := c.validate(); err != nil {
		return invalid(err.Error())
	}
	if c.request.Backup.Enabled && c.request.Backup.ProcessMax == 0 {
		c.request.Backup.ProcessMax = DefaultPgBackRestProcessMax
	}
	r := c.request
	unit, _ := MemoryUnitKB(r.TotalMemoryUnit)
	podKB := r.TotalMemory * unit
	podCPU := r.CPUNum * 1000
	alloc := &ResourceAllocation{PodMemory: FormatKB(podKB)}
	if podCPU > 0 {
		alloc.PodCPU = fmt.Sprintf("%dm", podCPU)
	}

	pgKB := int64(float64(podKB) * (1 - r.ProviderCostPct))
	pgCPU := int(float64(podCPU) * (1 - r.ProviderCostPct))
	availableCPU := pgCPU
	if !r.PGDedicated {
		pgKB -= PMMMemoryKB
		pgCPU -= PMMCPU
	}
	var warnings []string
	backupCPU, backupKB := 0, int64(0)
	if r.Backup.Enabled {
		var capped bool
		backupCPU, backupKB, capped = pgBackRestResources(r.Backup.ProcessMax, availableCPU)
		if capped {
			warnings = append(warnings, fmt.Sprintf("pgBackRest CPU is capped at %dm (%.0f%% of the pod); its %d parallel compression processes will be throttled and backups will take longer", backupCPU, PgBackRestMaxCPUPct*100, r.Backup.ProcessMax))
		}
		pgKB -= backupKB
		pgCPU -= backupCPU
	}
	alloc.PostgresMemory = FormatKB(pgKB)
	c.request.Allocation = alloc
	if pgKB < MinMemoryKB {
		return overloaded(fmt.Sprintf("postgres container has %s after provider cost and sidecars; at least 512MB is required", FormatKB(max(pgKB, 0))))
	}
	pgtuneCPU := 0
	if podCPU > 0 {
		if pgCPU < MinPostgresCPU {
			return overloaded(fmt.Sprintf("postgres container has %dm CPU after provider cost and sidecars; at least %dm is required", max(pgCPU, 0), MinPostgresCPU))
		}
		// Parallel settings count workers, not CPU time, so a fractional
		// core rounds up.
		pgtuneCPU = int(math.Ceil(float64(pgCPU) / 1000))
		alloc.PostgresCPU = fmt.Sprintf("%dm", pgCPU)
		alloc.PgTuneCPU = pgtuneCPU
	}

	tune := PgTune(PgTuneInput{
		Version:     r.PGVersion.Major,
		DBType:      r.DBType,
		RAMKB:       pgKB,
		CPU:         pgtuneCPU,
		Connections: r.Connections,
		HDType:      r.HDType,
		DBSize:      r.DBSize,
	})

	f := c.families
	pg := f[FamilyTypePostgres]
	for _, p := range tune.Parameters {
		put(pg, GroupNameConfiguration, p.Name, p.Value)
	}
	if podCPU == 0 {
		pgCPU = 0
	}
	// Every container in the instance pod gets request == limit so the pod
	// is in the Guaranteed QoS class.
	setResources(pg, pgCPU, pgCPU, pgKB, pgKB)
	if fam, ok := f[FamilyTypeMonitor]; ok {
		setResources(fam, PMMCPU, PMMCPU, PMMMemoryKB, PMMMemoryKB)
	}
	if fam, ok := f[FamilyTypeBackup]; ok {
		put(fam, GroupNameConfiguration, "process-max", strconv.Itoa(r.Backup.ProcessMax))
		setResources(fam, backupCPU, backupCPU, backupKB, backupKB)
	}
	if fam, ok := f[FamilyTypePgBouncer]; ok {
		setResources(fam, PgBouncerLimitCPU, PgBouncerRequestCPU, PgBouncerLimitMemKB, PgBouncerRequestMemKB)
	}

	pct := capacityPct(tune, pgKB)
	text := fmt.Sprintf("estimated memory utilization is %.0f%%", pct*100)
	if pct > 1 {
		return overloaded(fmt.Sprintf("safe capacity exceeded (%.0f%% estimated); reduce connections or increase totalmemory", pct*100))
	}
	warnings = append(tune.Warnings, warnings...)
	if podCPU == 0 {
		warnings = append(warnings, "cpunum was not given, so no CPU requests/limits are emitted and the instance pod will not get the Guaranteed QoS class")
	}
	msg := ResponseMessage{MType: OkI, MName: OkT, MText: text, Warnings: warnings}
	if pct > CloseLimitPct {
		msg.MType, msg.MName = ClosetolimitI, ClosetolimitT
	}
	return msg, f, nil
}

// pgBackRestResources sizes the pgBackRest sidecar for processMax parallel
// compression processes. podCPU is the pod CPU after provider cost; when it
// is zero (cpunum not given) no CPU is reserved.
func pgBackRestResources(processMax, podCPU int) (cpu int, memKB int64, capped bool) {
	memKB = PgBackRestBaseMemKB + int64(processMax)*PgBackRestProcessMemKB
	if podCPU <= 0 {
		return 0, memKB, false
	}
	cpu = PgBackRestBaseCPU + processMax*PgBackRestProcessCPU
	limit := max(PgBackRestMinCPU, int(float64(podCPU)*PgBackRestMaxCPUPct))
	if cpu > limit {
		return limit, memKB, true
	}
	return cpu, memKB, false
}

// capacityPct estimates steady-state memory use of the postgres container:
// shared memory plus one maintenance operation plus one work_mem allocation
// and a fixed private overhead for every backend and worker.
func capacityPct(t PgTuneResult, pgKB int64) float64 {
	backends := int64(t.MaxConnections + t.Workers)
	used := t.SharedBuffersKB + t.WalBuffersKB + t.MaintenanceWorkKB + backends*(t.WorkMemKB+ConnectionOverheadKB)
	return float64(used) / float64(pgKB)
}

// setResources writes Kubernetes requests and limits. CPU is in millicores
// and omitted when unknown; memory is in kB and emitted as bytes.
func setResources(f Family, limitCPU, requestCPU int, limitKB, requestKB int64) {
	if limitCPU > 0 {
		put(f, GroupNameResources, "request_cpu", fmt.Sprintf("%dm", requestCPU))
		put(f, GroupNameResources, "limit_cpu", fmt.Sprintf("%dm", limitCPU))
	}
	put(f, GroupNameResources, "request_memory", strconv.FormatInt(requestKB*1024, 10))
	put(f, GroupNameResources, "limit_memory", strconv.FormatInt(limitKB*1024, 10))
}
