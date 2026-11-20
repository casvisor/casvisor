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
	beego.Router("/api/change-password", &controllers.ApiController{}, "POST:ChangePassword")
	beego.Router("/api/get-mfa-setup", &controllers.ApiController{}, "POST:GetMfaSetup")
	beego.Router("/api/enable-mfa", &controllers.ApiController{}, "POST:EnableMfa")
	beego.Router("/api/disable-mfa", &controllers.ApiController{}, "POST:DisableMfa")
	beego.Router("/api/reset-local-user-mfa", &controllers.ApiController{}, "POST:ResetLocalUserMfa")

	beego.Router("/api/get-local-users", &controllers.ApiController{}, "GET:GetLocalUsers")
	beego.Router("/api/get-local-user", &controllers.ApiController{}, "GET:GetLocalUser")
	beego.Router("/api/update-local-user", &controllers.ApiController{}, "POST:UpdateLocalUser")
	beego.Router("/api/add-local-user", &controllers.ApiController{}, "POST:AddLocalUser")
	beego.Router("/api/delete-local-user", &controllers.ApiController{}, "POST:DeleteLocalUser")

	beego.Router("/api/get-records", &controllers.ApiController{}, "GET:GetRecords")
	beego.Router("/api/get-record", &controllers.ApiController{}, "GET:GetRecord")
	beego.Router("/api/add-record", &controllers.ApiController{}, "POST:AddRecord")
	beego.Router("/api/export-records", &controllers.ApiController{}, "GET:ExportRecords")

	beego.Router("/api/get-assets", &controllers.ApiController{}, "GET:GetAssets")
	beego.Router("/api/get-asset", &controllers.ApiController{}, "GET:GetAsset")
	beego.Router("/api/update-asset", &controllers.ApiController{}, "POST:UpdateAsset")
	beego.Router("/api/add-asset", &controllers.ApiController{}, "POST:AddAsset")
	beego.Router("/api/delete-asset", &controllers.ApiController{}, "POST:DeleteAsset")
	beego.Router("/api/test-asset", &controllers.ApiController{}, "POST:TestAsset")
	beego.Router("/api/import-assets", &controllers.ApiController{}, "POST:ImportAssets")
	beego.Router("/api/export-assets", &controllers.ApiController{}, "GET:ExportAssets")
	beego.Router("/api/move-assets", &controllers.ApiController{}, "POST:MoveAssets")

	beego.Router("/api/get-login-rules", &controllers.ApiController{}, "GET:GetLoginRules")
	beego.Router("/api/get-login-rule", &controllers.ApiController{}, "GET:GetLoginRule")
	beego.Router("/api/update-login-rule", &controllers.ApiController{}, "POST:UpdateLoginRule")
	beego.Router("/api/add-login-rule", &controllers.ApiController{}, "POST:AddLoginRule")
	beego.Router("/api/delete-login-rule", &controllers.ApiController{}, "POST:DeleteLoginRule")
	beego.Router("/api/get-login-reviews", &controllers.ApiController{}, "GET:GetLoginReviews")
	beego.Router("/api/review-login", &controllers.ApiController{}, "POST:ReviewLogin")
	beego.Router("/api/get-login-review-status", &controllers.ApiController{}, "GET:GetLoginReviewStatus")
	beego.Router("/api/cancel-login-review", &controllers.ApiController{}, "POST:CancelLoginReview")

	beego.Router("/api/get-asset-nodes", &controllers.ApiController{}, "GET:GetAssetNodes")
	beego.Router("/api/get-asset-node", &controllers.ApiController{}, "GET:GetAssetNode")
	beego.Router("/api/add-asset-node", &controllers.ApiController{}, "POST:AddAssetNode")
	beego.Router("/api/update-asset-node", &controllers.ApiController{}, "POST:UpdateAssetNode")
	beego.Router("/api/delete-asset-node", &controllers.ApiController{}, "POST:DeleteAssetNode")

	beego.Router("/api/get-asset-accounts", &controllers.ApiController{}, "GET:GetAssetAccounts")
	beego.Router("/api/get-asset-account", &controllers.ApiController{}, "GET:GetAssetAccount")
	beego.Router("/api/update-asset-account", &controllers.ApiController{}, "POST:UpdateAssetAccount")
	beego.Router("/api/add-asset-account", &controllers.ApiController{}, "POST:AddAssetAccount")
	beego.Router("/api/delete-asset-account", &controllers.ApiController{}, "POST:DeleteAssetAccount")
	beego.Router("/api/rotate-asset-account", &controllers.ApiController{}, "POST:RotateAssetAccount")
	beego.Router("/api/push-asset-account", &controllers.ApiController{}, "POST:PushAssetAccount")

	beego.Router("/api/get-permissions", &controllers.ApiController{}, "GET:GetPermissions")
	beego.Router("/api/get-permission", &controllers.ApiController{}, "GET:GetPermission")
	beego.Router("/api/update-permission", &controllers.ApiController{}, "POST:UpdatePermission")
	beego.Router("/api/add-permission", &controllers.ApiController{}, "POST:AddPermission")
	beego.Router("/api/delete-permission", &controllers.ApiController{}, "POST:DeletePermission")
	beego.Router("/api/get-users", &controllers.ApiController{}, "GET:GetUsers")

	beego.Router("/api/get-access-requests", &controllers.ApiController{}, "GET:GetAccessRequests")
	beego.Router("/api/get-requestable-assets", &controllers.ApiController{}, "GET:GetRequestableAssets")
	beego.Router("/api/add-access-request", &controllers.ApiController{}, "POST:AddAccessRequest")
	beego.Router("/api/cancel-access-request", &controllers.ApiController{}, "POST:CancelAccessRequest")
	beego.Router("/api/approve-access-request", &controllers.ApiController{}, "POST:ApproveAccessRequest")
	beego.Router("/api/reject-access-request", &controllers.ApiController{}, "POST:RejectAccessRequest")
	beego.Router("/api/revoke-access-request", &controllers.ApiController{}, "POST:RevokeAccessRequest")
	beego.Router("/api/delete-access-request", &controllers.ApiController{}, "POST:DeleteAccessRequest")
	beego.Router("/api/get-groups", &controllers.ApiController{}, "GET:GetGroups")

	beego.Router("/api/get-sessions", &controllers.ApiController{}, "GET:GetSessions")
	beego.Router("/api/get-session", &controllers.ApiController{}, "GET:GetConnSession")
	beego.Router("/api/update-session", &controllers.ApiController{}, "POST:UpdateSession")
	beego.Router("/api/add-session", &controllers.ApiController{}, "POST:AddSession")
	beego.Router("/api/delete-session", &controllers.ApiController{}, "POST:DeleteSession")
	beego.Router("/api/start-session", &controllers.ApiController{}, "POST:StartSession")
	beego.Router("/api/stop-session", &controllers.ApiController{}, "POST:StopSession")
	beego.Router("/api/get-session-monitor", &controllers.ApiController{}, "GET:GetSessionMonitor")
	beego.Router("/api/get-session-recording", &controllers.ApiController{}, "GET:GetSessionRecording")
	beego.Router("/api/export-sessions", &controllers.ApiController{}, "GET:ExportSessions")

	beego.Router("/api/get-network-gateways", &controllers.ApiController{}, "GET:GetNetworkGateways")
	beego.Router("/api/get-network-gateway", &controllers.ApiController{}, "GET:GetNetworkGateway")
	beego.Router("/api/update-network-gateway", &controllers.ApiController{}, "POST:UpdateNetworkGateway")
	beego.Router("/api/add-network-gateway", &controllers.ApiController{}, "POST:AddNetworkGateway")
	beego.Router("/api/delete-network-gateway", &controllers.ApiController{}, "POST:DeleteNetworkGateway")
	beego.Router("/api/test-network-gateway", &controllers.ApiController{}, "POST:TestNetworkGateway")
	beego.Router("/api/generate-network-gateway-token", &controllers.ApiController{}, "POST:GenerateNetworkGatewayToken")
	beego.Router("/api/revoke-network-gateway-token", &controllers.ApiController{}, "POST:RevokeNetworkGatewayToken")
	beego.Router("/api/connect-agent", &controllers.ApiController{}, "GET:ConnectAgent")

	beego.Router("/api/get-command-rules", &controllers.ApiController{}, "GET:GetCommandRules")
	beego.Router("/api/get-command-rule", &controllers.ApiController{}, "GET:GetCommandRule")
	beego.Router("/api/update-command-rule", &controllers.ApiController{}, "POST:UpdateCommandRule")
	beego.Router("/api/add-command-rule", &controllers.ApiController{}, "POST:AddCommandRule")
	beego.Router("/api/delete-command-rule", &controllers.ApiController{}, "POST:DeleteCommandRule")
	beego.Router("/api/get-commands", &controllers.ApiController{}, "GET:GetCommands")
	beego.Router("/api/export-commands", &controllers.ApiController{}, "GET:ExportCommands")

	beego.Router("/api/get-patch-statuses", &controllers.ApiController{}, "GET:GetPatchStatuses")
	beego.Router("/api/get-patch-status", &controllers.ApiController{}, "GET:GetPatchStatus")
	beego.Router("/api/get-patch-jobs", &controllers.ApiController{}, "GET:GetPatchJobs")
	beego.Router("/api/get-patch-job", &controllers.ApiController{}, "GET:GetPatchJob")
	beego.Router("/api/start-patch-job", &controllers.ApiController{}, "POST:StartPatchJob")

	beego.Router("/api/get-jobs", &controllers.ApiController{}, "GET:GetJobs")
	beego.Router("/api/get-job", &controllers.ApiController{}, "GET:GetJob")
	beego.Router("/api/start-job", &controllers.ApiController{}, "POST:StartJob")
	beego.Router("/api/rerun-job", &controllers.ApiController{}, "POST:RerunJob")
	beego.Router("/api/cancel-job", &controllers.ApiController{}, "POST:CancelJob")
	beego.Router("/api/delete-job", &controllers.ApiController{}, "POST:DeleteJob")
	beego.Router("/api/upload-job-file", &controllers.ApiController{}, "POST:UploadJobFile")

	beego.Router("/api/add-asset-tunnel", &controllers.ApiController{}, "POST:AddAssetTunnel")
	beego.Router("/api/get-asset-ssh", &controllers.ApiController{}, "GET:GetAssetSsh")
	beego.Router("/api/get-asset-vnc", &controllers.ApiController{}, "GET:GetAssetVnc")
	beego.Router("/api/get-asset-rdp", &controllers.ApiController{}, "GET:GetAssetRdp")
	beego.Router("/api/get-asset-desktop", &controllers.ApiController{}, "GET:GetAssetDesktop")
	beego.Router("/api/get-asset-telnet", &controllers.ApiController{}, "GET:GetAssetTelnet")
	beego.Router("/api/get-asset-database", &controllers.ApiController{}, "GET:GetAssetDatabase")
	beego.Router("/api/export-database-result", &controllers.ApiController{}, "GET:ExportDatabaseResult")
	beego.Router("/api/get-kubernetes-pods", &controllers.ApiController{}, "POST:GetKubernetesPods")
	beego.Router("/api/get-asset-kubernetes", &controllers.ApiController{}, "GET:GetAssetKubernetes")

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
