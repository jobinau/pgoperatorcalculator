package pgoperatorcalculator

import "testing"

func paramMap(r PgTuneResult) map[string]string {
	m := map[string]string{}
	for _, p := range r.Parameters {
		m[p.Name] = p.Value
	}
	return m
}

// The worked example from pgtune/parameter_calc.md.
func TestPgTuneWorkedExample(t *testing.T) {
	r := PgTune(PgTuneInput{Version: 18, DBType: DbTypeWeb, RAMKB: 16 * gB, CPU: 4, HDType: HDTypeSSD, DBSize: DBSizeMidRAM})
	want := [][2]string{
		{"max_connections", "200"}, {"shared_buffers", "4GB"}, {"effective_cache_size", "12GB"},
		{"maintenance_work_mem", "1GB"}, {"checkpoint_completion_target", "0.9"}, {"wal_buffers", "16MB"},
		{"default_statistics_target", "100"}, {"random_page_cost", "1.1"}, {"effective_io_concurrency", "200"},
		{"work_mem", "20560kB"}, {"huge_pages", "try"}, {"jit", "off"}, {"wal_compression", "lz4"},
		{"io_method", "io_uring"}, {"min_wal_size", "1GB"}, {"max_wal_size", "4GB"},
		{"max_worker_processes", "4"}, {"max_parallel_workers_per_gather", "2"},
		{"max_parallel_workers", "4"}, {"max_parallel_maintenance_workers", "2"},
	}
	if len(r.Parameters) != len(want) {
		t.Fatalf("got %d parameters: %+v", len(r.Parameters), r.Parameters)
	}
	for i, w := range want {
		if r.Parameters[i].Name != w[0] || r.Parameters[i].Value != w[1] {
			t.Errorf("parameter %d: got %s = %s, want %s = %s", i, r.Parameters[i].Name, r.Parameters[i].Value, w[0], w[1])
		}
	}
	if len(r.Warnings) != 2 {
		t.Errorf("want lz4 and io_uring warnings, got %v", r.Warnings)
	}
}

func TestPgTuneBranches(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   PgTuneInput
		want map[string]string // "" means omitted
	}{
		{"dw keeps random_page_cost at 4", PgTuneInput{17, DbTypeDW, 64 * gB, 8, 0, HDTypeNVMe, DBSizeGreaterRAM},
			map[string]string{"random_page_cost": "4", "default_statistics_target": "500", "maintenance_work_mem": "8GB", "autovacuum_work_mem": "2GB", "max_parallel_workers_per_gather": "4", "jit": "", "io_method": ""}},
		{"less_ram forces 1.1 on hdd", PgTuneInput{16, DbTypeOLTP, 8 * gB, 0, 0, HDTypeHDD, DBSizeLessRAM},
			map[string]string{"random_page_cost": "1.1", "effective_io_concurrency": "2", "max_worker_processes": ""}},
		{"desktop", PgTuneInput{14, DbTypeDesktop, 4 * gB, 2, 0, HDTypeSSD, DBSizeMidRAM},
			map[string]string{"shared_buffers": "256MB", "effective_cache_size": "1GB", "wal_compression": "on", "wal_level": "minimal", "max_wal_senders": "0", "huge_pages": "off", "jit": "", "min_wal_size": "100MB"}},
		{"work_mem floor", PgTuneInput{17, DbTypeWeb, 1 * gB, 0, 5000, HDTypeSSD, DBSizeMidRAM},
			map[string]string{"work_mem": "4MB", "max_connections": "5000"}},
		{"many cpus", PgTuneInput{17, DbTypeDW, 256 * gB, 64, 0, HDTypeSSD, DBSizeMidRAM},
			map[string]string{"autovacuum_max_workers": "5", "max_parallel_workers_per_gather": "32", "max_parallel_maintenance_workers": "4"}},
		{"pg10 has no parallel maintenance or jit", PgTuneInput{10, DbTypeWeb, 8 * gB, 8, 0, HDTypeSSD, DBSizeMidRAM},
			map[string]string{"max_parallel_maintenance_workers": "", "jit": "", "wal_compression": "on"}},
	} {
		got := paramMap(PgTune(tc.in))
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%s: %s = %q, want %q", tc.name, k, got[k], v)
			}
		}
	}
}

func TestFormatKB(t *testing.T) {
	for v, want := range map[int64]string{2 * gB: "2GB", 1536 * mB: "1536MB", 20560: "20560kB"} {
		if got := FormatKB(v); got != want {
			t.Errorf("FormatKB(%d) = %s, want %s", v, got, want)
		}
	}
}
