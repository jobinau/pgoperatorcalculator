package pgoperatorcalculator

import (
	"encoding/json"
	"fmt"
	"strings"
)

type Version struct {
	Major int `json:"major"`
	Minor int `json:"minor"`
	Patch int `json:"patch"`
}
type Versions struct {
	Min Version `json:"min"`
	Max Version `json:"max"`
}
type ResponseMessage struct {
	MType    int      `json:"type"`
	MName    string   `json:"name"`
	MText    string   `json:"text"`
	Warnings []string `json:"warnings,omitempty"`
}
type DBType struct {
	Name               string `json:"name"`
	Description        string `json:"description"`
	DefaultConnections int    `json:"defaultConnections"`
}
type Configuration struct {
	DBType          []DBType `json:"dbtype"`
	HDType          []string `json:"hdtype"`
	DBSize          []string `json:"dbsize"`
	TotalMemoryUnit []string `json:"totalmemoryunit"`
	MinTotalMemory  string   `json:"mintotalmemory"`
	MinConnections  int      `json:"minconnections"`
	Output          []string `json:"output"`
	PGVersions      Versions `json:"pgversions"`
}
type ConfigurationRequest struct {
	Output          string              `json:"output"`
	PGVersion       Version             `json:"pgversion"`
	DBType          string              `json:"dbtype"`
	TotalMemory     int64               `json:"totalmemory"`
	TotalMemoryUnit string              `json:"totalmemoryunit"`
	CPUNum          int                 `json:"cpunum"`
	Connections     int                 `json:"connections"`
	HDType          string              `json:"hdtype"`
	DBSize          string              `json:"dbsize"`
	ProviderCostPct float64             `json:"providercostpct"`
	PGDedicated     bool                `json:"pgdedicated"`
	Backup          BackupConfiguration `json:"backup"`
	PgBouncer       ComponentEnabled    `json:"pgbouncer"`
	Allocation      *ResourceAllocation `json:"allocation,omitempty"`
}
type ComponentEnabled struct {
	Enabled bool `json:"enabled"`
}
type BackupConfiguration struct {
	Enabled bool `json:"enabled"`
	// ProcessMax is pgBackRest's process-max; 0 means the default of 4.
	ProcessMax int `json:"processmax"`
}

// ResourceAllocation echoes how the pod was split before PgTune ran.
type ResourceAllocation struct {
	PodMemory      string `json:"podMemory"`
	PodCPU         string `json:"podCpu,omitempty"`
	PostgresMemory string `json:"postgresMemory"`
	PostgresCPU    string `json:"postgresCpu,omitempty"`
	PgTuneCPU      int    `json:"pgtuneCpu,omitempty"`
}
type Parameter struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (p Parameter) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}{p.Name, p.Value})
}

type GroupObj struct {
	Name       string               `json:"name"`
	Parameters map[string]Parameter `json:"parameters"`
}
type Family struct {
	Name   string              `json:"name"`
	Groups map[string]GroupObj `json:"groups"`
}

func (c *Configuration) Init() {
	c.DBType = []DBType{
		{DbTypeWeb, "Web application", 200},
		{DbTypeOLTP, "Online transaction processing", 300},
		{DbTypeDW, "Data warehouse", 40},
		{DbTypeDesktop, "Desktop application", 20},
		{DbTypeMixed, "Mixed type of application", 100},
	}
	c.HDType = []string{HDTypeHDD, HDTypeSSD, HDTypeSAN, HDTypeNVMe}
	c.DBSize = []string{DBSizeLessRAM, DBSizeMidRAM, DBSizeGreaterRAM}
	c.TotalMemoryUnit = []string{MemoryUnitMB, MemoryUnitGB, MemoryUnitTB}
	c.MinTotalMemory = "512MB"
	c.MinConnections = MinConnections
	c.Output = []string{ResultOutputFormatHuman, ResultOutputFormatJson}
	c.PGVersions = Versions{Version{MinPGVersion, 0, 0}, Version{MaxPGVersion, 99, 99}}
}

func (c Configuration) GetDBType(name string) (DBType, bool) {
	for _, t := range c.DBType {
		if t.Name == name {
			return t, true
		}
	}
	return DBType{}, false
}

// MemoryUnitKB returns the size of one unit in kB.
func MemoryUnitKB(unit string) (int64, error) {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case MemoryUnitMB:
		return mB, nil
	case MemoryUnitGB:
		return gB, nil
	case MemoryUnitTB:
		return tB, nil
	}
	return 0, fmt.Errorf("invalid totalmemoryunit %q, expected MB, GB or TB", unit)
}

func (c Configuration) Families(req ConfigurationRequest) map[string]Family {
	f := map[string]Family{FamilyTypePostgres: newFamily("postgres", GroupNameConfiguration, GroupNameResources)}
	if !req.PGDedicated {
		f[FamilyTypeMonitor] = newFamily("pmm-client", GroupNameResources)
	}
	if req.Backup.Enabled {
		f[FamilyTypeBackup] = newFamily("pgbackrest", GroupNameConfiguration, GroupNameResources)
	}
	if req.PgBouncer.Enabled {
		f[FamilyTypePgBouncer] = newFamily("pgbouncer", GroupNameResources)
	}
	return f
}
func newFamily(name string, groups ...string) Family {
	f := Family{Name: name, Groups: map[string]GroupObj{}}
	for _, g := range groups {
		f.Groups[g] = GroupObj{Name: g, Parameters: map[string]Parameter{}}
	}
	return f
}
func put(f Family, group, key, value string) {
	g := f.Groups[group]
	g.Parameters[key] = Parameter{Name: key, Value: value}
	f.Groups[group] = g
}

// FormatKB renders a kB value the way PgTune does: GB, MB or kB, whichever
// divides evenly first.
func FormatKB(v int64) string {
	switch {
	case v%gB == 0:
		return fmt.Sprintf("%dGB", v/gB)
	case v%mB == 0:
		return fmt.Sprintf("%dMB", v/mB)
	}
	return fmt.Sprintf("%dkB", v)
}
