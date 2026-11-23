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
	"encoding/csv"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/casvisor/casvisor/util"
)

const MaxImportAssets = 5000

// AssetImportColumns are the columns of an asset CSV file, in the order of the template and the export.
var AssetImportColumns = []string{"name", "displayName", "description", "protocol", "host", "port", "username", "password", "privateKey", "database", "os", "tag", "nodes", "networkGateway"}

var assetProtocols = []string{"SSH", "RDP", "VNC", "Telnet", ProtocolMysql, ProtocolPostgresql, ProtocolSqlServer, ProtocolOracle, ProtocolRedis, ProtocolMongodb, ProtocolKubernetes, ProtocolWeb, ProtocolDesktop}

var assetDefaultPorts = map[string]int{"SSH": 22, "RDP": 3389, "VNC": 5900, "Telnet": 23, ProtocolMysql: 3306, ProtocolPostgresql: 5432, ProtocolSqlServer: 1433, ProtocolOracle: 1521, ProtocolRedis: 6379, ProtocolMongodb: 27017, ProtocolKubernetes: KubernetesDefaultPort, ProtocolWeb: 80}

const (
	AssetImportCreate = "create"
	AssetImportUpdate = "update"
	AssetImportSkip   = "skip"
	AssetImportError  = "error"
)

type AssetImportResult struct {
	Row     int      `json:"row"`
	Name    string   `json:"name"`
	Action  string   `json:"action"`
	Message string   `json:"message"`
	Changes []string `json:"changes"`
}

type AssetImportSummary struct {
	Created      int `json:"created"`
	Updated      int `json:"updated"`
	Skipped      int `json:"skipped"`
	Failed       int `json:"failed"`
	CreatedNodes int `json:"createdNodes"`
}

// getAssetImportColumn maps a header cell, such as "Display name" or "display_name", to a column.
func getAssetImportColumn(header string) string {
	key := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(header, "\uFEFF")))
	key = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(key)
	for _, column := range AssetImportColumns {
		if strings.ToLower(column) == key {
			return column
		}
	}
	aliases := map[string]string{"ip": "host", "address": "host", "user": "username", "node": "nodes", "gateway": "networkGateway", "key": "privateKey"}
	return aliases[key]
}

// splitAssetNodePaths splits a "nodes" cell, whose paths are separated by ";", "|" or new lines.
func splitAssetNodePaths(cell string) []string {
	res := []string{}
	for _, path := range strings.FieldsFunc(cell, func(r rune) bool { return r == ';' || r == '|' || r == '\n' || r == '\r' }) {
		path = strings.TrimSpace(path)
		if path != "" {
			res = append(res, path)
		}
	}
	return res
}

func normalizeAssetProtocol(protocol string) (string, error) {
	for _, p := range assetProtocols {
		if strings.EqualFold(p, strings.TrimSpace(protocol)) {
			return p, nil
		}
	}
	aliases := map[string]string{"postgres": ProtocolPostgresql, "mssql": ProtocolSqlServer, "sql server": ProtocolSqlServer, "mongo": ProtocolMongodb}
	if p, ok := aliases[strings.ToLower(strings.TrimSpace(protocol))]; ok {
		return p, nil
	}
	return "", fmt.Errorf("unknown protocol: %s, it should be one of: %s", protocol, strings.Join(assetProtocols, ", "))
}

func checkImportedAssetName(name string) error {
	if name == "" {
		return fmt.Errorf("the name can not be empty")
	}
	if strings.ContainsAny(name, "/@: \t") {
		return fmt.Errorf("the name: %s can not contain \"/\", \"@\", \":\" or spaces", name)
	}
	if len(name) > 100 {
		return fmt.Errorf("the name: %s is longer than 100 characters", name)
	}
	return nil
}

// ParseAssetCsv reads a CSV file of assets, the first line being the header.
func ParseAssetCsv(text string) ([]string, [][]string, error) {
	text = strings.TrimPrefix(text, "\uFEFF")
	reader := csv.NewReader(strings.NewReader(text))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	return parseAssetRecords(records)
}

// ParseAssetXlsx reads the first sheet of an Excel workbook of assets, with the columns of the CSV file.
func ParseAssetXlsx(data []byte) ([]string, [][]string, error) {
	records, err := ReadXlsxRows(data)
	if err != nil {
		return nil, nil, err
	}
	return parseAssetRecords(records)
}

func parseAssetRecords(records [][]string) ([]string, [][]string, error) {
	if len(records) == 0 {
		return nil, nil, fmt.Errorf("the file is empty")
	}

	columns := []string{}
	hasName := false
	for _, header := range records[0] {
		column := getAssetImportColumn(header)
		if column != "" && slices.Contains(columns, column) {
			return nil, nil, fmt.Errorf("the column: %s appears twice", column)
		}
		columns = append(columns, column)
		hasName = hasName || column == "name"
	}
	if !hasName {
		return nil, nil, fmt.Errorf("the file has no \"name\" column, its first line should be the header: %s", strings.Join(AssetImportColumns, ","))
	}

	rows := [][]string{}
	for _, record := range records[1:] {
		empty := true
		for _, cell := range record {
			if strings.TrimSpace(cell) != "" {
				empty = false
				break
			}
		}
		if !empty {
			rows = append(rows, record)
		}
	}
	if len(rows) > MaxImportAssets {
		return nil, nil, fmt.Errorf("the file has %d assets, %d at most can be imported at a time", len(rows), MaxImportAssets)
	}
	return columns, rows, nil
}

// ImportAssets adds the assets of the rows and, when update is true, updates the existing ones, or
// only tells what it would do when dryRun is true. A row with an error is skipped and the others go on.
// A column left out of the file keeps the old value of an existing asset, and so does an empty
// password or private key.
func ImportAssets(owner string, columns []string, rows [][]string, update bool, dryRun bool) ([]*AssetImportResult, *AssetImportSummary, error) {
	results := []*AssetImportResult{}
	summary := &AssetImportSummary{}

	nodes, err := GetAssetNodes(owner)
	if err != nil {
		return nil, nil, err
	}
	oldNodes := []string{}
	for _, node := range nodes {
		oldNodes = append(oldNodes, node.Name)
	}
	gateways, err := GetNetworkGateways(owner)
	if err != nil {
		return nil, nil, err
	}
	gatewayNames := map[string]bool{}
	for _, gateway := range gateways {
		gatewayNames[gateway.Name] = true
	}

	seen := map[string]int{}
	for i, row := range rows {
		result := &AssetImportResult{Row: i + 2}
		results = append(results, result)

		cells := map[string]string{}
		for j, column := range columns {
			if column != "" && j < len(row) {
				cells[column] = row[j]
			}
		}
		has := func(column string) bool {
			_, ok := cells[column]
			return ok
		}
		get := func(column string) string {
			value := strings.TrimSpace(cells[column])
			// undo the quote that an export puts before a cell that a spreadsheet would run as a formula
			if strings.HasPrefix(value, "'") && len(value) > 1 && strings.ContainsRune("=+-@", rune(value[1])) {
				value = value[1:]
			}
			return value
		}

		fail := func(err error) {
			result.Action = AssetImportError
			result.Message = err.Error()
			summary.Failed++
		}

		result.Name = get("name")
		err = checkImportedAssetName(result.Name)
		if err != nil {
			fail(err)
			continue
		}
		if row, ok := seen[result.Name]; ok {
			fail(fmt.Errorf("the asset: %s is already in row %d", result.Name, row))
			continue
		}
		seen[result.Name] = result.Row

		old, err := getAsset(owner, result.Name)
		if err != nil {
			return nil, nil, err
		}
		if old != nil && !update {
			result.Action = AssetImportSkip
			result.Message = "the asset already exists"
			summary.Skipped++
			continue
		}

		asset := &Asset{}
		if old != nil {
			*asset = *old
		} else {
			asset = &Asset{
				Owner:       owner,
				Name:        result.Name,
				CreatedTime: util.GetCurrentTime(),
				DisplayName: result.Name,
				Protocol:    "SSH",
				Os:          "Linux",
			}
		}

		rowErr := func() error {
			if has("protocol") && get("protocol") != "" {
				asset.Protocol, err = normalizeAssetProtocol(get("protocol"))
				if err != nil {
					return err
				}
			}
			for _, column := range []string{"displayName", "description", "host", "username", "database", "os", "tag", "networkGateway"} {
				if !has(column) {
					continue
				}
				value := get(column)
				switch column {
				case "displayName":
					if value == "" {
						value = asset.Name
					}
					asset.DisplayName = value
				case "description":
					asset.Description = value
				case "host":
					asset.Host = value
				case "username":
					asset.Username = value
				case "database":
					asset.Database = value
				case "os":
					asset.Os = value
				case "tag":
					asset.Tag = value
				case "networkGateway":
					if value != "" && !gatewayNames[value] {
						return fmt.Errorf("the network gateway: %s does not exist", value)
					}
					asset.NetworkGateway = value
				}
			}
			if has("password") && cells["password"] != "" {
				asset.Password = cells["password"]
			}
			if has("privateKey") && strings.TrimSpace(cells["privateKey"]) != "" {
				asset.PrivateKey = strings.ReplaceAll(strings.TrimSpace(cells["privateKey"]), `\n`, "\n")
			}

			if has("port") && get("port") != "" {
				port, err := strconv.Atoi(get("port"))
				if err != nil || port <= 0 || port > 65535 {
					return fmt.Errorf("invalid port: %s", get("port"))
				}
				asset.Port = port
			} else if old == nil || asset.Protocol != old.Protocol {
				asset.Port = assetDefaultPorts[asset.Protocol]
			}

			if asset.Protocol != ProtocolDesktop && asset.Host == "" {
				return fmt.Errorf("the host can not be empty")
			}
			err := asset.normalizeAddress()
			if err != nil {
				return err
			}
			if asset.Protocol == ProtocolDesktop && asset.NetworkGateway == "" {
				return fmt.Errorf("a Desktop asset needs its Casvisor Desktop device in the networkGateway column")
			}

			if has("nodes") {
				names := []string{}
				for _, path := range splitAssetNodePaths(cells["nodes"]) {
					name, created, err := resolveAssetNodePath(owner, &nodes, path, !dryRun)
					if err != nil {
						return err
					}
					if created {
						result.Changes = append(result.Changes, "new node: "+path)
					}
					if !slices.Contains(names, name) {
						names = append(names, name)
					}
				}
				asset.Nodes = names
			}
			return nil
		}()
		if rowErr != nil {
			fail(rowErr)
			continue
		}

		if old != nil {
			err = asset.checkKeptSecrets(old, asset.Password == old.Password, asset.PrivateKey == old.PrivateKey)
			if err != nil {
				fail(err)
				continue
			}

			result.Action = AssetImportUpdate
			result.Changes = append(getAssetChanges(old, asset), result.Changes...)
			if len(result.Changes) == 0 {
				result.Action = AssetImportSkip
				result.Message = "nothing to change"
				summary.Skipped++
				continue
			}

			if asset.Password == old.Password && old.Password != "" {
				asset.Password = "***"
			}
			if asset.PrivateKey == old.PrivateKey && old.PrivateKey != "" {
				asset.PrivateKey = "***"
			}
		} else {
			result.Action = AssetImportCreate
		}

		if !dryRun {
			if old != nil {
				asset.UpdatedTime = util.GetCurrentTime()
				_, err = UpdateAsset(asset.GetId(), asset)
			} else {
				asset.UpdatedTime = asset.CreatedTime
				_, err = AddAsset(asset)
			}
			if err != nil {
				fail(err)
				continue
			}
		} else if asset.NetworkGateway != "" {
			err = asset.checkNetworkGateway()
			if err != nil {
				fail(err)
				continue
			}
		}

		if result.Action == AssetImportCreate {
			summary.Created++
		} else {
			summary.Updated++
		}
	}

	for _, node := range nodes {
		if strings.HasPrefix(node.Name, "?") || !slices.Contains(oldNodes, node.Name) {
			summary.CreatedNodes++
		}
	}
	return results, summary, nil
}

func getAssetChanges(old *Asset, asset *Asset) []string {
	res := []string{}
	fields := []struct {
		name     string
		old, new string
	}{
		{"displayName", old.DisplayName, asset.DisplayName},
		{"description", old.Description, asset.Description},
		{"protocol", old.Protocol, asset.Protocol},
		{"host", old.Host, asset.Host},
		{"port", strconv.Itoa(old.Port), strconv.Itoa(asset.Port)},
		{"username", old.Username, asset.Username},
		{"database", old.Database, asset.Database},
		{"os", old.Os, asset.Os},
		{"tag", old.Tag, asset.Tag},
		{"networkGateway", old.NetworkGateway, asset.NetworkGateway},
	}
	for _, field := range fields {
		if field.old != field.new {
			res = append(res, fmt.Sprintf("%s: %s → %s", field.name, field.old, field.new))
		}
	}
	if old.Password != asset.Password {
		res = append(res, "password")
	}
	if old.PrivateKey != asset.PrivateKey {
		res = append(res, "privateKey")
	}
	if !slices.Equal(old.Nodes, asset.Nodes) && (len(old.Nodes) > 0 || len(asset.Nodes) > 0) {
		res = append(res, "nodes")
	}
	return res
}

// GetAssetCsvRow is the row of the asset in the export, without its password and private key.
func GetAssetCsvRow(asset *Asset, nodeMap map[string]*AssetNode) []string {
	paths := []string{}
	for _, name := range asset.Nodes {
		paths = append(paths, GetAssetNodePath(nodeMap, name))
	}
	return []string{
		asset.Name, asset.DisplayName, asset.Description, asset.Protocol, asset.Host, strconv.Itoa(asset.Port), asset.Username,
		"", "", asset.Database, asset.Os, asset.Tag, strings.Join(paths, ";"), asset.NetworkGateway,
	}
}

func GetAssetNodeMap(owner string) (map[string]*AssetNode, error) {
	nodes, err := GetAssetNodes(owner)
	if err != nil {
		return nil, err
	}
	return getAssetNodeMap(nodes), nil
}
