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

package main

import (
	"net/http"
	_ "net/http/pprof"
	"os"

	"github.com/beego/beego"
	"github.com/beego/beego/logs"
	"github.com/beego/beego/plugins/cors"
	_ "github.com/beego/beego/session/redis"
	"github.com/casvisor/casvisor/agent"
	"github.com/casvisor/casvisor/authz"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/controllers"
	"github.com/casvisor/casvisor/demo"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/recording"
	"github.com/casvisor/casvisor/routers"
	"github.com/casvisor/casvisor/sshgateway"
	"github.com/casvisor/casvisor/task"
	"github.com/casvisor/casvisor/util"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "agent" {
		agent.Main(os.Args[2:])
		return
	}

	object.InitAdapter()
	err := object.EncryptAssetCredentials()
	if err != nil {
		panic(err)
	}
	err = object.InitLocalAdmin()
	if err != nil {
		panic(err)
	}
	object.InitPatchJobs()
	recording.StartRetention()
	err = controllers.ClearSessionDrives()
	if err != nil {
		logs.Warning("failed to clear the drives of earlier RDP sessions: %s", err.Error())
	}
	authz.InitAuthz()
	util.InitIpDb()
	util.InitParser()

	beego.InsertFilter("*", beego.BeforeRouter, cors.Allow(&cors.Options{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "DELETE", "PUT", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "X-Requested-With", "Content-Type", "Accept"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
	}))

	beego.SetStaticPath("/swagger", "swagger")
	// https://studygolang.com/articles/2303
	beego.InsertFilter("/", beego.BeforeRouter, routers.TransparentStatic) // must has this for default page
	beego.InsertFilter("/*", beego.BeforeRouter, routers.TransparentStatic)
	beego.InsertFilter("*", beego.BeforeRouter, routers.RefreshLocalUser)
	beego.InsertFilter("*", beego.BeforeRouter, routers.ApiFilter)
	beego.InsertFilter("*", beego.BeforeRouter, routers.RecordMessage)
	beego.InsertFilter("*", beego.AfterExec, routers.AfterRecordMessage, false)

	if beego.AppConfig.String("redisEndpoint") == "" {
		beego.BConfig.WebConfig.Session.SessionProvider = "file"
		beego.BConfig.WebConfig.Session.SessionProviderConfig = "./tmp"
	} else {
		beego.BConfig.WebConfig.Session.SessionProvider = "redis"
		beego.BConfig.WebConfig.Session.SessionProviderConfig = beego.AppConfig.String("redisEndpoint")
	}
	beego.BConfig.WebConfig.Session.SessionGCMaxLifetime = 3600 * 24 * 365

	task.NewTicker().SetupTicker()
	demo.StartDemoTargets()
	sshgateway.Start()
	object.StartLiveSessionChecker()

	// The profiles of a running server, on an address of its own that is
	// only set where it is needed, such as 127.0.0.1:6060.
	if addr := conf.GetConfigString("pprofAddr"); addr != "" {
		go func() {
			logs.Error(http.ListenAndServe(addr, nil))
		}()
	}

	beego.Run()
}
