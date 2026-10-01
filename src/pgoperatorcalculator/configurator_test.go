package pgoperatorcalculator

import (
	"strings"
	"testing"
)

func calc(t *testing.T, req ConfigurationRequest) (error, ResponseMessage, map[string]Family, ConfigurationRequest) {
	t.Helper()
	var conf Configuration
	conf.Init()
	var c PGOperatorCalculator
	c.Init(req, conf)
	e, msg, f := c.GetCalculate()
	return e, msg, f, c.IncomingRequest
}

func baseRequest() ConfigurationRequest {
	return ConfigurationRequest{
		PGVersion: Version{18, 0, 0}, DBType: DbTypeWeb, TotalMemory: 16, TotalMemoryUnit: MemoryUnitGB,
		CPUNum: 4, HDType: HDTypeSSD, DBSize: DBSizeMidRAM,
	}
}

func TestDedicatedMatchesPgTune(t *testing.T) {
	req := baseRequest()
	req.PGDedicated = true
	e, msg, f, _ := calc(t, req)
	if e != nil || msg.MType != OkI {
		t.Fatalf("%v %+v", e, msg)
	}
	if _, ok := f[FamilyTypeMonitor]; ok {
		t.Fatal("monitor should be omitted in dedicated mode")
	}
	conf := f[FamilyTypePostgres].Groups[GroupNameConfiguration].Parameters
	if conf["shared_buffers"].Value != "4GB" || conf["work_mem"].Value != "20560kB" {
		t.Fatalf("dedicated pod should match the PgTune worked example: %+v", conf)
	}
	res := f[FamilyTypePostgres].Groups[GroupNameResources].Parameters
	if res["limit_memory"].Value != "17179869184" || res["limit_cpu"].Value != "4000m" || res["request_cpu"].Value != "4000m" || res["request_memory"].Value != "17179869184" {
		t.Fatalf("unexpected postgres resources: %+v", res)
	}
}

func TestSidecarsAreDeducted(t *testing.T) {
	req := baseRequest()
	req.Backup.Enabled = true
	req.PgBouncer.Enabled = true
	e, msg, f, echo := calc(t, req)
	if e != nil {
		t.Fatal(e)
	}
	// 16GB - 256MB PMM - (128MB + 4 x 64MB) pgBackRest;
	// 4000m - 400m PMM - 1000m pgBackRest (capped at 25% of the pod).
	if echo.Allocation.PostgresMemory != "15744MB" || echo.Allocation.PostgresCPU != "2600m" || echo.Allocation.PgTuneCPU != 3 {
		t.Fatalf("unexpected allocation: %+v", echo.Allocation)
	}
	if echo.Backup.ProcessMax != DefaultPgBackRestProcessMax {
		t.Fatalf("processmax default not echoed: %+v", echo.Backup)
	}
	conf := f[FamilyTypePostgres].Groups[GroupNameConfiguration].Parameters
	if conf["shared_buffers"].Value != "3936MB" {
		t.Fatalf("PgTune should size the postgres container only: %+v", conf)
	}
	if _, ok := conf["max_worker_processes"]; ok {
		t.Fatal("3 cores left for postgres should omit parallel settings")
	}
	for _, name := range []string{FamilyTypeMonitor, FamilyTypeBackup, FamilyTypePgBouncer} {
		if len(f[name].Groups[GroupNameResources].Parameters) != 4 {
			t.Fatalf("missing resources for %s", name)
		}
	}
	backup := f[FamilyTypeBackup].Groups
	if backup[GroupNameResources].Parameters["limit_memory"].Value != "402653184" ||
		backup[GroupNameResources].Parameters["limit_cpu"].Value != "1000m" ||
		backup[GroupNameConfiguration].Parameters["process-max"].Value != "4" {
		t.Fatalf("unexpected pgBackRest sizing: %+v", backup)
	}
	if !strings.Contains(strings.Join(msg.Warnings, "\n"), "pgBackRest CPU is capped at 1000m") {
		t.Fatalf("missing pgBackRest cap warning: %v", msg.Warnings)
	}
}

func TestPgBackRestScalesWithProcessMax(t *testing.T) {
	for _, tc := range []struct {
		cpus, processMax int
		cpu, mem         string
		capped           bool
	}{
		{32, 0, "4200m", "402653184", false}, // default 4 processes, uncapped
		{32, 8, "8000m", "671088640", true},  // 8200m wanted, 25% of 32 cores
		{64, 8, "8200m", "671088640", false},
		{8, 1, "1200m", "201326592", false},
		{2, 4, "500m", "402653184", true}, // minimum CPU on a small pod
	} {
		req := baseRequest()
		req.CPUNum = tc.cpus
		req.TotalMemory = 64
		req.Backup = BackupConfiguration{Enabled: true, ProcessMax: tc.processMax}
		e, msg, f, _ := calc(t, req)
		if e != nil {
			t.Fatalf("%+v: %v", tc, e)
		}
		res := f[FamilyTypeBackup].Groups[GroupNameResources].Parameters
		capped := strings.Contains(strings.Join(msg.Warnings, "\n"), "pgBackRest CPU is capped")
		if res["limit_cpu"].Value != tc.cpu || res["request_cpu"].Value != tc.cpu || res["limit_memory"].Value != tc.mem || capped != tc.capped {
			t.Errorf("%+v: got cpu=%s mem=%s capped=%v", tc, res["limit_cpu"].Value, res["limit_memory"].Value, capped)
		}
	}
}

func TestInstancePodIsGuaranteed(t *testing.T) {
	req := baseRequest()
	req.Backup.Enabled = true
	req.PgBouncer.Enabled = true
	_, msg, f, _ := calc(t, req)
	for _, name := range []string{FamilyTypePostgres, FamilyTypeMonitor, FamilyTypeBackup} {
		res := f[name].Groups[GroupNameResources].Parameters
		if res["request_cpu"].Value == "" || res["request_memory"].Value == "" ||
			res["request_cpu"].Value != res["limit_cpu"].Value || res["request_memory"].Value != res["limit_memory"].Value {
			t.Errorf("%s requests must equal limits for Guaranteed QoS: %+v", name, res)
		}
	}
	for _, w := range msg.Warnings {
		if strings.Contains(w, "Guaranteed") {
			t.Errorf("unexpected QoS warning with cpunum set: %s", w)
		}
	}
}

func TestProviderCost(t *testing.T) {
	req := baseRequest()
	req.PGDedicated = true
	req.ProviderCostPct = 0.25
	_, _, f, echo := calc(t, req)
	if echo.Allocation.PostgresMemory != "12GB" || echo.Allocation.PostgresCPU != "3000m" {
		t.Fatalf("unexpected allocation: %+v", echo.Allocation)
	}
	if f[FamilyTypePostgres].Groups[GroupNameConfiguration].Parameters["shared_buffers"].Value != "3GB" {
		t.Fatal("provider cost should reduce the PgTune memory")
	}
}

func TestCPUOptional(t *testing.T) {
	req := baseRequest()
	req.CPUNum = 0
	e, _, f, _ := calc(t, req)
	if e != nil {
		t.Fatal(e)
	}
	res := f[FamilyTypePostgres].Groups[GroupNameResources].Parameters
	if _, ok := res["limit_cpu"]; ok {
		t.Fatalf("cpu resources should be omitted without cpunum: %+v", res)
	}
	if _, ok := f[FamilyTypePostgres].Groups[GroupNameConfiguration].Parameters["max_worker_processes"]; ok {
		t.Fatal("parallel settings should be omitted without cpunum")
	}
	if res["request_memory"].Value != res["limit_memory"].Value {
		t.Fatalf("memory request must still equal limit: %+v", res)
	}
	_, msg, _, _ := calc(t, req)
	if !strings.Contains(strings.Join(msg.Warnings, "\n"), "Guaranteed QoS") {
		t.Fatalf("missing QoS warning: %v", msg.Warnings)
	}
}

func TestOverload(t *testing.T) {
	for name, mutate := range map[string]func(*ConfigurationRequest){
		"connections": func(r *ConfigurationRequest) { r.TotalMemory = 1; r.Connections = 2000 },
		"memory":      func(r *ConfigurationRequest) { r.TotalMemory = 600; r.TotalMemoryUnit = MemoryUnitMB },
		"cpu":         func(r *ConfigurationRequest) { r.CPUNum = 1; r.Backup.Enabled = true },
	} {
		req := baseRequest()
		mutate(&req)
		e, msg, f, _ := calc(t, req)
		if e == nil || msg.MType != OverutilizingI || len(f) != 0 {
			t.Errorf("%s: expected overload, got %v %+v", name, e, msg)
		}
	}
}

func TestInvalidRequests(t *testing.T) {
	for name, mutate := range map[string]func(*ConfigurationRequest){
		"version":     func(r *ConfigurationRequest) { r.PGVersion.Major = 9 },
		"dbtype":      func(r *ConfigurationRequest) { r.DBType = "olap" },
		"memory":      func(r *ConfigurationRequest) { r.TotalMemory = 256; r.TotalMemoryUnit = MemoryUnitMB },
		"unit":        func(r *ConfigurationRequest) { r.TotalMemoryUnit = "PB" },
		"connections": func(r *ConfigurationRequest) { r.Connections = 10 },
		"hdtype":      func(r *ConfigurationRequest) { r.HDType = "tape" },
		"dbsize":      func(r *ConfigurationRequest) { r.DBSize = "" },
		"cost":        func(r *ConfigurationRequest) { r.ProviderCostPct = 1 },
		"processmax":  func(r *ConfigurationRequest) { r.Backup = BackupConfiguration{Enabled: true, ProcessMax: 33} },
	} {
		req := baseRequest()
		mutate(&req)
		e, msg, _, _ := calc(t, req)
		if e == nil || msg.MType != ErrorexecI {
			t.Errorf("%s: expected invalid request, got %v %+v", name, e, msg)
		}
	}
}

func TestHumanOutputIsPostgresqlConf(t *testing.T) {
	req := baseRequest()
	req.DBType = " OLTP "
	req.TotalMemoryUnit = "gb"
	var conf Configuration
	conf.Init()
	var c PGOperatorCalculator
	c.Init(req, conf)
	_, msg, f := c.GetCalculate()
	out, _ := c.GetHumanOutput(msg, c.IncomingRequest, f)
	s := out.String()
	if !strings.Contains(s, "# WARNING\n") || !strings.Contains(s, "[postgres.configuration]\nmax_connections = 300\nshared_buffers = 4032MB\n") {
		t.Fatalf("unexpected human output:\n%s", s)
	}
}
