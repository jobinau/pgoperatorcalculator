package pgoperatorcalculator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type PGOperatorCalculator struct {
	IncomingRequest ConfigurationRequest
	Conf            Configuration
	configurator    Configurator
}

func (m *PGOperatorCalculator) Init(req ConfigurationRequest, conf Configuration) ConfigurationRequest {
	req.DBType = strings.ToLower(strings.TrimSpace(req.DBType))
	req.HDType = strings.ToLower(strings.TrimSpace(req.HDType))
	req.DBSize = strings.ToLower(strings.TrimSpace(req.DBSize))
	req.TotalMemoryUnit = strings.ToUpper(strings.TrimSpace(req.TotalMemoryUnit))
	m.IncomingRequest = req
	m.Conf = conf
	return m.IncomingRequest
}
func (m *PGOperatorCalculator) GetSupportedLayouts() Configuration {
	var c Configuration
	c.Init()
	return c
}

// GetCalculate runs the calculation. The returned request echoes the input
// together with the resource allocation the calculator decided on.
func (m *PGOperatorCalculator) GetCalculate() (error, ResponseMessage, map[string]Family) {
	m.configurator = Configurator{request: m.IncomingRequest, conf: m.Conf, families: m.Conf.Families(m.IncomingRequest)}
	msg, f, e := m.configurator.calculate()
	m.IncomingRequest = m.configurator.request
	if f == nil {
		f = map[string]Family{}
	}
	return e, msg, f
}
func (m *PGOperatorCalculator) GetJSONOutput(msg ResponseMessage, req ConfigurationRequest, f map[string]Family) (bytes.Buffer, error) {
	var b bytes.Buffer
	x := struct {
		Message  ResponseMessage      `json:"message"`
		Incoming ConfigurationRequest `json:"incoming"`
		Answer   map[string]Family    `json:"answer"`
	}{msg, req, f}
	out, e := json.MarshalIndent(x, "", "  ")
	b.Write(out)
	return b, e
}

// GetHumanOutput renders an ini-like view. The postgres configuration group
// is valid postgresql.conf syntax and keeps PgTune's parameter order.
func (m *PGOperatorCalculator) GetHumanOutput(msg ResponseMessage, req ConfigurationRequest, f map[string]Family) (bytes.Buffer, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, "[message]\nname = %s\ntype = %d\ntext = %s\n", msg.MName, msg.MType, msg.MText)
	if len(msg.Warnings) > 0 {
		b.WriteString("# WARNING\n")
		for _, w := range msg.Warnings {
			fmt.Fprintf(&b, "# - %s\n", w)
		}
	}
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		groups := f[k].Groups
		gkeys := make([]string, 0, len(groups))
		for x := range groups {
			gkeys = append(gkeys, x)
		}
		sort.Strings(gkeys)
		for _, x := range gkeys {
			fmt.Fprintf(&b, "[%s.%s]\n", f[k].Name, x)
			for _, p := range m.orderedParameters(k, groups[x]) {
				fmt.Fprintf(&b, "%s = %s\n", p.Name, p.Value)
			}
		}
	}
	return b, nil
}
func (m *PGOperatorCalculator) orderedParameters(family string, g GroupObj) []Parameter {
	var out []Parameter
	if family == FamilyTypePostgres && g.Name == GroupNameConfiguration {
		for _, name := range parameterOrder {
			if p, ok := g.Parameters[name]; ok {
				out = append(out, p)
			}
		}
		return out
	}
	names := make([]string, 0, len(g.Parameters))
	for n := range g.Parameters {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		out = append(out, g.Parameters[n])
	}
	return out
}
func (m *PGOperatorCalculator) GetFamily(name string) (Family, error) {
	f, ok := m.configurator.families[name]
	if !ok {
		return Family{}, fmt.Errorf("invalid family %q", name)
	}
	return f, nil
}

var parameterOrder = []string{
	"max_connections", "shared_buffers", "effective_cache_size", "maintenance_work_mem",
	"checkpoint_completion_target", "wal_buffers", "default_statistics_target", "random_page_cost",
	"effective_io_concurrency", "work_mem", "huge_pages", "jit", "wal_compression",
	"autovacuum_max_workers", "autovacuum_work_mem", "io_method", "min_wal_size", "max_wal_size",
	"max_worker_processes", "max_parallel_workers_per_gather", "max_parallel_workers",
	"max_parallel_maintenance_workers", "wal_level", "max_wal_senders",
}
