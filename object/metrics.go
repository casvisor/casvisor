// Copyright 2026 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package object

import (
	"fmt"
	"sort"
	"strings"
)

type metricRow struct {
	Owner string
	Label string
	Count int64
}

type metricWriter struct {
	sb strings.Builder
}

func (w *metricWriter) family(name string, kind string, help string) {
	w.sb.WriteString(fmt.Sprintf("# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind))
}

func (w *metricWriter) sample(name string, labels map[string]string, value int64) {
	w.sb.WriteString(name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		pairs := make([]string, 0, len(keys))
		for _, key := range keys {
			value := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(labels[key])
			pairs = append(pairs, fmt.Sprintf("%s=%q", key, value))
		}
		w.sb.WriteString("{" + strings.Join(pairs, ",") + "}")
	}
	w.sb.WriteString(fmt.Sprintf(" %d\n", value))
}

func countBy(bean interface{}, column string, where string, args ...interface{}) ([]metricRow, error) {
	rows := []metricRow{}
	session := adapter.engine.Table(bean).Select(fmt.Sprintf("owner, %s as label, count(*) as count", column))
	if where != "" {
		session = session.Where(where, args...)
	}
	err := session.GroupBy("owner, " + column).Find(&rows)
	return rows, err
}

// GetMetricsText returns the state of Casvisor in the Prometheus text format.
func GetMetricsText() (string, error) {
	w := &metricWriter{}
	w.family("casvisor_up", "gauge", "Whether Casvisor is up.")
	w.sample("casvisor_up", nil, 1)

	families := []struct {
		name   string
		help   string
		label  string
		bean   interface{}
		column string
		where  string
		args   []interface{}
	}{
		{"casvisor_sessions_connected", "Sessions connected now.", "protocol", &Session{}, "protocol", "status = ?", []interface{}{Connected}},
		{"casvisor_sessions", "Sessions by status, connected or not.", "status", &Session{}, "status", "", nil},
		{"casvisor_commands", "Commands recorded, by the action of the rule they matched, empty for none.", "action", &Command{}, "action", "", nil},
		{"casvisor_assets", "Assets by protocol.", "protocol", &Asset{}, "protocol", "", nil},
		{"casvisor_access_requests", "Access requests by status.", "status", &AccessRequest{}, "status", "", nil},
	}
	for _, family := range families {
		rows, err := countBy(family.bean, family.column, family.where, family.args...)
		if err != nil {
			return "", err
		}
		w.family(family.name, "gauge", family.help)
		for _, row := range rows {
			w.sample(family.name, map[string]string{"organization": row.Owner, family.label: row.Label}, row.Count)
		}
	}

	gateways := []*NetworkGateway{}
	err := adapter.engine.Where("agent_key <> ''").Find(&gateways)
	if err != nil {
		return "", err
	}
	w.family("casvisor_network_agent_up", "gauge", "Whether the agent of a network gateway is connected.")
	for _, gateway := range gateways {
		up := int64(0)
		if getAgent(gateway.GetId()) != nil {
			up = 1
		}
		w.sample("casvisor_network_agent_up", map[string]string{"organization": gateway.Owner, "gateway": gateway.Name}, up)
	}

	return w.sb.String(), nil
}
