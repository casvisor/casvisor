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

package controllers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/casvisor/casvisor/object"
)

// GetAssetNodes
// @Title GetAssetNodes
// @Tag Asset Node API
// @Description get the asset tree with how many assets each node has; a user who is not an admin only gets the nodes of the assets they may connect to
// @Param   owner     query    string  true        "The owner of the nodes"
// @Success 200 {array} object.AssetNode The Response object
// @router /get-asset-nodes [get]
func (c *ApiController) GetAssetNodes() {
	owner := c.Input().Get("owner")
	user := c.GetSessionUser()

	var assets []*object.Asset
	var err error
	isAdmin := c.isAdmin()
	if isAdmin {
		assets, err = object.GetAssets(owner)
	} else {
		owner = user.Owner
		assets, err = object.GetAssets(owner)
		if err == nil {
			assets, err = object.FilterAllowedAssets(user, assets, c.getAuthzClientIp())
		}
	}
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	nodes, unassigned, err := object.GetAssetNodeTree(owner, assets, !isAdmin)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(nodes, map[string]int{"total": len(assets), "unassigned": unassigned})
}

// GetAssetNode
// @Title GetAssetNode
// @Tag Asset Node API
// @Description get an asset node
// @Param   id     query    string  true        "The id ( owner/name ) of the node"
// @Success 200 {object} object.AssetNode The Response object
// @router /get-asset-node [get]
func (c *ApiController) GetAssetNode() {
	node, err := object.GetAssetNode(c.Input().Get("id"))
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(node)
}

// AddAssetNode
// @Title AddAssetNode
// @Tag Asset Node API
// @Description add an asset node
// @Param   body    body   object.AssetNode  true        "The details of the node"
// @Success 200 {object} controllers.Response The Response object
// @router /add-asset-node [post]
func (c *ApiController) AddAssetNode() {
	var node object.AssetNode
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &node)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(node.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.AddAssetNode(&node))
	c.ServeJSON()
}

// UpdateAssetNode
// @Title UpdateAssetNode
// @Tag Asset Node API
// @Description rename or move an asset node; the assets and permissions of a renamed node follow it
// @Param   id     query    string  true        "The id ( owner/name ) of the node"
// @Param   body    body   object.AssetNode  true        "The details of the node"
// @Success 200 {object} controllers.Response The Response object
// @router /update-asset-node [post]
func (c *ApiController) UpdateAssetNode() {
	id := c.Input().Get("id")

	var node object.AssetNode
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &node)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(node.Owner) {
		return
	}

	affected, err := object.UpdateAssetNode(id, &node)
	if err == nil {
		go object.CheckLiveSessions()
	}
	c.Data["json"] = wrapActionResponse(affected, err)
	c.ServeJSON()
}

// DeleteAssetNode
// @Title DeleteAssetNode
// @Tag Asset Node API
// @Description delete an asset node that has no child nodes, no assets and no permissions
// @Param   body    body   object.AssetNode  true        "The node"
// @Success 200 {object} controllers.Response The Response object
// @router /delete-asset-node [post]
func (c *ApiController) DeleteAssetNode() {
	var node object.AssetNode
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &node)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(node.Owner) {
		return
	}

	c.Data["json"] = wrapActionResponse(object.DeleteAssetNode(&node))
	c.ServeJSON()
}

type moveAssetsForm struct {
	Owner  string   `json:"owner"`
	Assets []string `json:"assets"`
	Nodes  []string `json:"nodes"`
	Mode   string   `json:"mode"`
}

// MoveAssets
// @Title MoveAssets
// @Tag Asset Node API
// @Description add assets to nodes (mode "add"), take them out of nodes ("remove") or put them in exactly the nodes ("set")
// @Param   body    body   controllers.moveAssetsForm  true        "The assets, the nodes and the mode"
// @Success 200 {object} controllers.Response The Response object
// @router /move-assets [post]
func (c *ApiController) MoveAssets() {
	var form moveAssetsForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(form.Owner) {
		return
	}

	count, err := object.MoveAssetsToNodes(form.Owner, form.Assets, form.Nodes, form.Mode)
	if count > 0 {
		go object.CheckLiveSessions()
	}
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	c.ResponseOk(count)
}

type importAssetsForm struct {
	Owner string `json:"owner"`
	Csv   string `json:"csv"`
	// Xlsx is an Excel workbook in base64, read from its first sheet, instead of the CSV file
	Xlsx   string `json:"xlsx"`
	Update bool   `json:"update"`
	DryRun bool   `json:"dryRun"`
}

// ImportAssets
// @Title ImportAssets
// @Tag Asset API
// @Description add assets from a CSV file or the first sheet of an Excel workbook, and update the existing ones when update is true; with dryRun it only tells what it would do
// @Param   body    body   controllers.importAssetsForm  true        "The CSV file or the workbook in base64, and the options"
// @Success 200 {object} controllers.Response The Response object
// @router /import-assets [post]
func (c *ApiController) ImportAssets() {
	var form importAssetsForm
	err := json.Unmarshal(c.Ctx.Input.RequestBody, &form)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	if !c.checkOwner(form.Owner) {
		return
	}

	var columns []string
	var rows [][]string
	if form.Xlsx != "" {
		data, decodeErr := base64.StdEncoding.DecodeString(form.Xlsx)
		if decodeErr != nil {
			c.ResponseError(decodeErr.Error())
			return
		}
		columns, rows, err = object.ParseAssetXlsx(data)
	} else {
		columns, rows, err = object.ParseAssetCsv(form.Csv)
	}
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	results, summary, err := object.ImportAssets(form.Owner, columns, rows, form.Update, form.DryRun)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	if !form.DryRun {
		if summary.Updated > 0 {
			go object.CheckLiveSessions()
		}

		organization, userName := form.Owner, ""
		if user := c.GetSessionUser(); user != nil {
			organization, userName = user.Owner, user.Name
		}
		object.AddAuditRecord(&object.AuditEvent{
			Organization: organization,
			User:         userName,
			ClientIp:     c.getClientIp(),
			UserAgent:    c.getUserAgent(),
			Method:       "Import",
			Action:       "import-assets",
			Detail: fmt.Sprintf("Imported %d rows of assets: %d created, %d updated, %d skipped, %d failed, %d new nodes",
				len(rows), summary.Created, summary.Updated, summary.Skipped, summary.Failed, summary.CreatedNodes),
		})
	}

	c.ResponseOk(results, summary)
}

// ExportAssets
// @Title ExportAssets
// @Tag Asset API
// @Description download the assets that match the filter of the asset list as CSV, or as an Excel workbook with format=xlsx, without passwords and private keys, in the format of the import
// @Param   owner     query    string  true        "The owner of the assets"
// @Param   node      query    string  false       "Only the assets in this node or below it, @none for the assets in no node"
// @Param   field     query    string  false       "The field to filter on"
// @Param   value     query    string  false       "The value of the filter"
// @Param   format    query    string  false       "csv (default) or xlsx"
// @Success 200 {string} string The CSV or xlsx file
// @router /export-assets [get]
func (c *ApiController) ExportAssets() {
	query, ok := c.getExportQuery()
	if !ok {
		return
	}

	assets, err := object.GetFilteredAssets(query.owner, c.Input().Get("node"), query.field, query.value, query.sortField, query.sortOrder)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}
	nodeMap, err := object.GetAssetNodeMap(query.owner)
	if err != nil {
		c.ResponseError(err.Error())
		return
	}

	c.exportTable("assets", object.AssetImportColumns, func(write func(row []string) error) error {
		for _, asset := range assets {
			err := write(object.GetAssetCsvRow(asset, nodeMap))
			if err != nil {
				return err
			}
		}
		return nil
	})
}
