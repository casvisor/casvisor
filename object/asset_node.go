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
	"slices"
	"sort"
	"strings"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

// MaxAssetNodeDepth keeps a chain of nodes within what the role manager of Casbin follows.
const MaxAssetNodeDepth = 8

// AssetNodeNone filters the assets that are in no node.
const AssetNodeNone = "@none"

// AssetNode is a folder of the asset tree. An asset can be in several nodes, and a permission
// on a node covers the assets in it and in all of its descendants.
type AssetNode struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`
	Parent      string `xorm:"varchar(100) index" json:"parent"`

	AssetCount int `xorm:"-" json:"assetCount"`
}

func GetAssetNodes(owner string) ([]*AssetNode, error) {
	nodes := []*AssetNode{}
	err := adapter.engine.Asc("created_time").Find(&nodes, &AssetNode{Owner: owner})
	if err != nil {
		return nodes, err
	}
	return nodes, nil
}

func getAssetNode(owner string, name string) (*AssetNode, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	node := AssetNode{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&node)
	if err != nil {
		return nil, err
	}
	if !existed {
		return nil, nil
	}
	return &node, nil
}

func GetAssetNode(id string) (*AssetNode, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getAssetNode(owner, name)
}

func (node *AssetNode) GetId() string {
	return fmt.Sprintf("%s/%s", node.Owner, node.Name)
}

func checkAssetNodeName(name string) error {
	if name == "" {
		return fmt.Errorf("the name of a node can not be empty")
	}
	if strings.ContainsAny(name, "/@ \t\r\n") {
		return fmt.Errorf("the name of a node: %s can not contain \"/\", \"@\" or spaces", name)
	}
	return nil
}

func getAssetNodeMap(nodes []*AssetNode) map[string]*AssetNode {
	res := map[string]*AssetNode{}
	for _, node := range nodes {
		res[node.Name] = node
	}
	return res
}

// getAssetNodeDepth is how many nodes the chain from the root to the node has, or -1 for a cycle.
func getAssetNodeDepth(nodeMap map[string]*AssetNode, name string) int {
	depth := 0
	visited := map[string]bool{}
	for name != "" {
		if visited[name] {
			return -1
		}
		visited[name] = true
		node := nodeMap[name]
		if node == nil {
			break
		}
		depth++
		name = node.Parent
	}
	return depth
}

func getAssetNodeHeight(nodes []*AssetNode, name string) int {
	height := 1
	for _, node := range nodes {
		if node.Parent == name && node.Name != name {
			height = max(height, getAssetNodeHeight(nodes, node.Name)+1)
		}
	}
	return height
}

// GetAssetNodeDescendants returns the node and all nodes below it.
func GetAssetNodeDescendants(nodes []*AssetNode, name string) map[string]bool {
	res := map[string]bool{name: true}
	for changed := true; changed; {
		changed = false
		for _, node := range nodes {
			if res[node.Parent] && !res[node.Name] {
				res[node.Name] = true
				changed = true
			}
		}
	}
	return res
}

// GetAssetNodePath returns the display names from the root to the node, joined with "/".
func GetAssetNodePath(nodeMap map[string]*AssetNode, name string) string {
	tokens := []string{}
	visited := map[string]bool{}
	for name != "" && !visited[name] {
		visited[name] = true
		node := nodeMap[name]
		if node == nil {
			tokens = append(tokens, name)
			break
		}
		tokens = append(tokens, getAssetNodeTitle(node))
		name = node.Parent
	}
	slices.Reverse(tokens)
	return strings.Join(tokens, "/")
}

func getAssetNodeTitle(node *AssetNode) string {
	if node.DisplayName != "" {
		return node.DisplayName
	}
	return node.Name
}

func (node *AssetNode) check(nodes []*AssetNode, oldName string) error {
	err := checkAssetNodeName(node.Name)
	if err != nil {
		return err
	}
	if strings.Contains(node.DisplayName, "/") {
		return fmt.Errorf("the display name of a node: %s can not contain \"/\"", node.DisplayName)
	}

	nodeMap := map[string]*AssetNode{}
	for _, n := range nodes {
		if n.Name != oldName {
			nodeMap[n.Name] = n
		}
	}
	if node.Name != oldName && nodeMap[node.Name] != nil {
		return fmt.Errorf("the node: %s already exists", node.Name)
	}

	if node.Parent == "" {
		return nil
	}
	if node.Parent == node.Name || node.Parent == oldName {
		return fmt.Errorf("a node can not be its own parent")
	}
	if nodeMap[node.Parent] == nil {
		return fmt.Errorf("the parent node: %s does not exist", node.Parent)
	}
	if oldName != "" && GetAssetNodeDescendants(nodes, oldName)[node.Parent] {
		return fmt.Errorf("a node can not be moved under its own descendant")
	}

	height := 1
	if oldName != "" {
		height = getAssetNodeHeight(nodes, oldName)
	}
	if getAssetNodeDepth(nodeMap, node.Parent)+height > MaxAssetNodeDepth {
		return fmt.Errorf("the asset tree can not be deeper than %d levels", MaxAssetNodeDepth)
	}
	return nil
}

func AddAssetNode(node *AssetNode) (bool, error) {
	nodes, err := GetAssetNodes(node.Owner)
	if err != nil {
		return false, err
	}

	err = node.check(nodes, "")
	if err != nil {
		return false, err
	}

	if node.CreatedTime == "" {
		node.CreatedTime = util.GetCurrentTime()
	}
	node.UpdatedTime = util.GetCurrentTime()
	affected, err := adapter.engine.Insert(node)
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

func UpdateAssetNode(id string, node *AssetNode) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getAssetNode(owner, name)
	if err != nil {
		return false, err
	} else if old == nil {
		return false, nil
	}

	nodes, err := GetAssetNodes(owner)
	if err != nil {
		return false, err
	}

	node.Owner = owner
	err = node.check(nodes, name)
	if err != nil {
		return false, err
	}

	node.CreatedTime = old.CreatedTime
	node.UpdatedTime = util.GetCurrentTime()
	affected, err := adapter.engine.ID(core.PK{owner, name}).AllCols().Update(node)
	if err != nil {
		return false, err
	}

	if affected != 0 && node.Name != name {
		err = renameAssetNode(owner, name, node.Name)
		if err != nil {
			return false, err
		}
	}
	return affected != 0, nil
}

func renameAssetNode(owner, oldName, newName string) error {
	_, err := adapter.engine.Where("owner = ? and parent = ?", owner, oldName).Cols("parent").Update(&AssetNode{Parent: newName})
	if err != nil {
		return err
	}

	assets, err := GetAssets(owner)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if !slices.Contains(asset.Nodes, oldName) {
			continue
		}
		err = updateAssetNodes(asset, replaceName(asset.Nodes, oldName, newName))
		if err != nil {
			return err
		}
	}

	permissions, err := GetPermissions(owner)
	if err != nil {
		return err
	}
	for _, permission := range permissions {
		if !slices.Contains(permission.Nodes, oldName) {
			continue
		}
		permission.Nodes = replaceName(permission.Nodes, oldName, newName)
		_, err = adapter.engine.ID(core.PK{owner, permission.Name}).Cols("nodes").Update(permission)
		if err != nil {
			return err
		}
	}

	schedules := []*JobSchedule{}
	err = adapter.engine.Find(&schedules, &JobSchedule{Owner: owner})
	if err != nil {
		return err
	}
	for _, schedule := range schedules {
		if !slices.Contains(schedule.AssetNodes, oldName) {
			continue
		}
		schedule.AssetNodes = replaceName(schedule.AssetNodes, oldName, newName)
		_, err = adapter.engine.ID(core.PK{owner, schedule.Name}).Cols("asset_nodes").Update(schedule)
		if err != nil {
			return err
		}
	}
	return nil
}

func replaceName(names []string, oldName, newName string) []string {
	res := []string{}
	for _, name := range names {
		if name == oldName {
			name = newName
		}
		if !slices.Contains(res, name) {
			res = append(res, name)
		}
	}
	return res
}

func DeleteAssetNode(node *AssetNode) (bool, error) {
	nodes, err := GetAssetNodes(node.Owner)
	if err != nil {
		return false, err
	}
	for _, n := range nodes {
		if n.Parent == node.Name {
			return false, fmt.Errorf("the node: %s still has child nodes, delete or move them first", node.Name)
		}
	}

	assets, err := GetAssets(node.Owner)
	if err != nil {
		return false, err
	}
	count := 0
	for _, asset := range assets {
		if slices.Contains(asset.Nodes, node.Name) {
			count++
		}
	}
	if count > 0 {
		return false, fmt.Errorf("the node: %s still has %d assets, move them out first", node.Name, count)
	}

	permissions, err := GetPermissions(node.Owner)
	if err != nil {
		return false, err
	}
	for _, permission := range permissions {
		if slices.Contains(permission.Nodes, node.Name) {
			return false, fmt.Errorf("the node: %s is used by the permission: %s", node.Name, permission.Name)
		}
	}

	affected, err := adapter.engine.ID(core.PK{node.Owner, node.Name}).Delete(&AssetNode{})
	if err != nil {
		return false, err
	}
	return affected != 0, nil
}

func updateAssetNodes(asset *Asset, nodes []string) error {
	asset.Nodes = nodes
	_, err := adapter.engine.ID(core.PK{asset.Owner, asset.Name}).Cols("nodes").Update(&Asset{Nodes: nodes})
	return err
}

// checkAssetNodes removes the duplicates and makes sure that every node exists.
func checkAssetNodes(owner string, names []string) ([]string, error) {
	res := []string{}
	if len(names) == 0 {
		return res, nil
	}

	nodes, err := GetAssetNodes(owner)
	if err != nil {
		return nil, err
	}
	nodeMap := getAssetNodeMap(nodes)
	for _, name := range names {
		if nodeMap[name] == nil {
			return nil, fmt.Errorf("the node: %s does not exist in the organization: %s", name, owner)
		}
		if !slices.Contains(res, name) {
			res = append(res, name)
		}
	}
	return res, nil
}

// IsAssetInNodes tells whether the asset is in any of the nodes, AssetNodeNone matching an asset in no node.
func IsAssetInNodes(asset *Asset, nodes map[string]bool) bool {
	if nodes[AssetNodeNone] && len(asset.Nodes) == 0 {
		return true
	}
	for _, name := range asset.Nodes {
		if nodes[name] {
			return true
		}
	}
	return false
}

// FilterAssetsByNode keeps the assets in the node or below it.
func FilterAssetsByNode(owner string, assets []*Asset, node string) ([]*Asset, error) {
	if node == "" {
		return assets, nil
	}

	names := map[string]bool{AssetNodeNone: true}
	if node != AssetNodeNone {
		nodes, err := GetAssetNodes(owner)
		if err != nil {
			return nil, err
		}
		names = GetAssetNodeDescendants(nodes, node)
	}

	res := []*Asset{}
	for _, asset := range assets {
		if IsAssetInNodes(asset, names) {
			res = append(res, asset)
		}
	}
	return res, nil
}

// GetAssetNodeTree returns the nodes with how many of the assets are in each of them or below it.
// When onlyUsed is true, only the nodes with any of the assets are returned, with their ancestors.
func GetAssetNodeTree(owner string, assets []*Asset, onlyUsed bool) ([]*AssetNode, int, error) {
	nodes, err := GetAssetNodes(owner)
	if err != nil {
		return nil, 0, err
	}
	nodeMap := getAssetNodeMap(nodes)

	counted := map[string]map[string]bool{}
	unassigned := 0
	for _, asset := range assets {
		if len(asset.Nodes) == 0 {
			unassigned++
			continue
		}
		for _, name := range asset.Nodes {
			visited := map[string]bool{}
			for name != "" && !visited[name] && nodeMap[name] != nil {
				visited[name] = true
				if counted[name] == nil {
					counted[name] = map[string]bool{}
				}
				counted[name][asset.Name] = true
				name = nodeMap[name].Parent
			}
		}
	}

	res := []*AssetNode{}
	for _, node := range nodes {
		node.AssetCount = len(counted[node.Name])
		if onlyUsed && node.AssetCount == 0 {
			continue
		}
		res = append(res, node)
	}

	sort.SliceStable(res, func(i, j int) bool {
		return strings.ToLower(getAssetNodeTitle(res[i])) < strings.ToLower(getAssetNodeTitle(res[j]))
	})
	return res, unassigned, nil
}

// MoveAssetsToNodes adds the assets to the nodes ("add"), takes them out of the nodes ("remove") or
// puts them in exactly the nodes ("set").
func MoveAssetsToNodes(owner string, assetNames []string, nodeNames []string, mode string) (int, error) {
	if mode != "add" && mode != "remove" && mode != "set" {
		return 0, fmt.Errorf("invalid mode: %s", mode)
	}

	nodeNames, err := checkAssetNodes(owner, nodeNames)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, assetName := range assetNames {
		asset, err := getAsset(owner, assetName)
		if err != nil {
			return count, err
		}
		if asset == nil {
			return count, fmt.Errorf("the asset: %s does not exist in the organization: %s", assetName, owner)
		}

		nodes := []string{}
		switch mode {
		case "add":
			nodes = append(nodes, asset.Nodes...)
			for _, name := range nodeNames {
				if !slices.Contains(nodes, name) {
					nodes = append(nodes, name)
				}
			}
		case "remove":
			for _, name := range asset.Nodes {
				if !slices.Contains(nodeNames, name) {
					nodes = append(nodes, name)
				}
			}
		case "set":
			nodes = nodeNames
		}

		if slices.Equal(nodes, asset.Nodes) {
			continue
		}
		err = updateAssetNodes(asset, nodes)
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// resolveAssetNodePath finds the node of a path of display names (or names) from the root, such as
// "Beijing/Production". Missing nodes are created when create is true, and they are added to nodes.
func resolveAssetNodePath(owner string, nodes *[]*AssetNode, path string, create bool) (string, bool, error) {
	tokens := []string{}
	for _, token := range strings.Split(strings.Trim(strings.TrimSpace(path), "/"), "/") {
		token = strings.TrimSpace(token)
		if token != "" {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 {
		return "", false, fmt.Errorf("empty node path")
	}
	if len(tokens) > MaxAssetNodeDepth {
		return "", false, fmt.Errorf("the node path: %s is deeper than %d levels", path, MaxAssetNodeDepth)
	}

	parent := ""
	created := false
	for _, token := range tokens {
		matches := []*AssetNode{}
		for _, node := range *nodes {
			if node.Parent == parent && (node.DisplayName == token || (node.DisplayName == "" && node.Name == token)) {
				matches = append(matches, node)
			}
		}
		if len(matches) == 0 {
			for _, node := range *nodes {
				if node.Parent == parent && node.Name == token {
					matches = append(matches, node)
				}
			}
		}
		if len(matches) > 1 {
			return "", false, fmt.Errorf("the node path: %s is ambiguous, %d nodes are named: %s", path, len(matches), token)
		}
		if len(matches) == 1 {
			parent = matches[0].Name
			continue
		}

		if !create {
			node := &AssetNode{Owner: owner, Name: "?" + parent + "/" + token, DisplayName: token, Parent: parent}
			*nodes = append(*nodes, node)
			parent = node.Name
			created = true
			continue
		}

		node := &AssetNode{
			Owner:       owner,
			Name:        fmt.Sprintf("node_%s", strings.ReplaceAll(util.GenerateId(), "-", "")[:12]),
			CreatedTime: util.GetCurrentTime(),
			DisplayName: token,
			Parent:      parent,
		}
		_, err := AddAssetNode(node)
		if err != nil {
			return "", false, err
		}
		*nodes = append(*nodes, node)
		parent = node.Name
		created = true
	}
	return parent, created, nil
}
