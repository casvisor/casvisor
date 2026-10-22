// Copyright 2023 The casbin Authors. All Rights Reserved.
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

package routers

import (
	"github.com/beego/beego"
	"github.com/casvisor/casvisor/controllers"
)

func init() {
	initAPI()
}

func initAPI() {
	ns := beego.NewNamespace("/api",
		beego.NSInclude(
			&controllers.ApiController{},
		),
	)
	beego.AddNamespace(ns)

	beego.Router("/api/signin", &controllers.ApiController{}, "POST:Signin")
	beego.Router("/api/signout", &controllers.ApiController{}, "POST:Signout")
	beego.Router("/api/get-account", &controllers.ApiController{}, "GET:GetAccount")

	beego.Router("/api/get-records", &controllers.ApiController{}, "GET:GetRecords")
	beego.Router("/api/get-record", &controllers.ApiController{}, "GET:GetRecord")
	beego.Router("/api/update-record", &controllers.ApiController{}, "POST:UpdateRecord")
	beego.Router("/api/add-record", &controllers.ApiController{}, "POST:AddRecord")
	beego.Router("/api/delete-record", &controllers.ApiController{}, "POST:DeleteRecord")

	beego.Router("/api/get-assets", &controllers.ApiController{}, "GET:GetAssets")
	beego.Router("/api/get-asset", &controllers.ApiController{}, "GET:GetAsset")
	beego.Router("/api/update-asset", &controllers.ApiController{}, "POST:UpdateAsset")
	beego.Router("/api/add-asset", &controllers.ApiController{}, "POST:AddAsset")
	beego.Router("/api/delete-asset", &controllers.ApiController{}, "POST:DeleteAsset")

	beego.Router("/api/get-permissions", &controllers.ApiController{}, "GET:GetPermissions")
	beego.Router("/api/get-permission", &controllers.ApiController{}, "GET:GetPermission")
	beego.Router("/api/update-permission", &controllers.ApiController{}, "POST:UpdatePermission")
	beego.Router("/api/add-permission", &controllers.ApiController{}, "POST:AddPermission")
	beego.Router("/api/delete-permission", &controllers.ApiController{}, "POST:DeletePermission")
	beego.Router("/api/get-users", &controllers.ApiController{}, "GET:GetUsers")
	beego.Router("/api/get-groups", &controllers.ApiController{}, "GET:GetGroups")

	beego.Router("/api/get-sessions", &controllers.ApiController{}, "GET:GetSessions")
	beego.Router("/api/get-session", &controllers.ApiController{}, "GET:GetConnSession")
	beego.Router("/api/update-session", &controllers.ApiController{}, "POST:UpdateSession")
	beego.Router("/api/add-session", &controllers.ApiController{}, "POST:AddSession")
	beego.Router("/api/delete-session", &controllers.ApiController{}, "POST:DeleteSession")
	beego.Router("/api/start-session", &controllers.ApiController{}, "POST:StartSession")
	beego.Router("/api/stop-session", &controllers.ApiController{}, "POST:StopSession")
	beego.Router("/api/get-session-monitor", &controllers.ApiController{}, "GET:GetSessionMonitor")

	beego.Router("/api/get-command-rules", &controllers.ApiController{}, "GET:GetCommandRules")
	beego.Router("/api/get-command-rule", &controllers.ApiController{}, "GET:GetCommandRule")
	beego.Router("/api/update-command-rule", &controllers.ApiController{}, "POST:UpdateCommandRule")
	beego.Router("/api/add-command-rule", &controllers.ApiController{}, "POST:AddCommandRule")
	beego.Router("/api/delete-command-rule", &controllers.ApiController{}, "POST:DeleteCommandRule")
	beego.Router("/api/get-commands", &controllers.ApiController{}, "GET:GetCommands")

	beego.Router("/api/add-asset-tunnel", &controllers.ApiController{}, "POST:AddAssetTunnel")
	beego.Router("/api/get-asset-tunnel", &controllers.ApiController{}, "GET:GetAssetTunnel")
	beego.Router("/api/get-asset-ssh", &controllers.ApiController{}, "GET:GetAssetSsh")
	beego.Router("/api/get-asset-vnc", &controllers.ApiController{}, "GET:GetAssetVnc")
	beego.Router("/api/get-asset-rdp", &controllers.ApiController{}, "GET:GetAssetRdp")

	beego.Router("/api/get-session-files", &controllers.ApiController{}, "GET:GetSessionFiles")
	beego.Router("/api/download-session-file", &controllers.ApiController{}, "GET:DownloadSessionFile")
	beego.Router("/api/upload-session-file", &controllers.ApiController{}, "POST:UploadSessionFile")
	beego.Router("/api/create-session-dir", &controllers.ApiController{}, "POST:CreateSessionDir")
	beego.Router("/api/rename-session-file", &controllers.ApiController{}, "POST:RenameSessionFile")
	beego.Router("/api/delete-session-file", &controllers.ApiController{}, "POST:DeleteSessionFile")

	beego.Router("/api/get-ssh-keys", &controllers.ApiController{}, "GET:GetSshKeys")
	beego.Router("/api/add-ssh-key", &controllers.ApiController{}, "POST:AddSshKey")
	beego.Router("/api/delete-ssh-key", &controllers.ApiController{}, "POST:DeleteSshKey")
	beego.Router("/api/get-ssh-gateway", &controllers.ApiController{}, "GET:GetSshGateway")
}
