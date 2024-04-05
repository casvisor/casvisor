// Copyright 2024 The casbin Authors. All Rights Reserved.
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

package client

import (
	"github.com/casvisor/casvisor/proxy/tunnel"
	"io"
	"time"

	"github.com/beego/beego/logs"
)

type Client struct {
	Name          string
	LocalPort     int
	RemoteAddr    string
	RemotePort    int
	wantProxyApps map[string]*tunnel.AppInfo
	onProxyApps   map[string]*tunnel.AppInfo
	heartbeatChan chan *tunnel.Message // when get heartbeat msg, put msg in
}

func NewClient(name string, remoteAddr string, remotePort int, appInfo *tunnel.AppInfo) *Client {
	client := &Client{
		Name:          name,
		RemoteAddr:    remoteAddr,
		RemotePort:    remotePort,
		heartbeatChan: make(chan *tunnel.Message, 1),
		onProxyApps:   make(map[string]*tunnel.AppInfo),
		wantProxyApps: make(map[string]*tunnel.AppInfo),
	}

	if appInfo != nil {
		client.wantProxyApps[appInfo.Name] = appInfo
	}
	return client
}

func (c *Client) Run() {
	conn, err := tunnel.Dial(c.RemoteAddr, c.RemotePort)
	if err != nil {
		logs.Error("Dial server error.", err)
	}

	c.sendInitAppMsg(conn)
	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			if err == io.EOF {
				logs.Info("Name [%s], c is dead!", c.Name)
				return
			}
			logs.Error("Read message from server error.", err)
			break
		}

		switch msg.Type {
		case tunnel.TypeServerHeartbeat:
			c.heartbeatChan <- msg
		case tunnel.TypeAppMsg:
			c.storeServerApp(conn, msg)
		case tunnel.TypeAppWaitBind:
			go c.handleBindMsg(msg)
		}
	}
}

func (c *Client) sendInitAppMsg(conn *tunnel.Conn) {
	if c.wantProxyApps == nil {
		logs.Error("has no app c to proxy")
	}

	// 通知server开始监听这些app
	msg := tunnel.NewMessage(tunnel.TypeInitApp, "", c.Name, c.wantProxyApps)
	if err := conn.SendMessage(msg); err != nil {
		logs.Error("send init app msg to server.", err)
		return
	}
}

func (c *Client) storeServerApp(conn *tunnel.Conn, msg *tunnel.Message) {
	if msg.Meta == nil {
		logs.Error("has no app to proxy")
	}

	for name, app := range msg.Meta.(map[string]interface{}) {
		appServer := app.(map[string]interface{})
		c.onProxyApps[name] = &tunnel.AppInfo{
			Name:       appServer["Name"].(string),
			ListenPort: int(appServer["ListenPort"].(float64)),
		}
	}

	logs.Info("---------- Sever ----------")
	for name, app := range c.onProxyApps {
		logs.Info("[%s]:\t%s:%d", name, conn.GetRemoteIP(), app.ListenPort)
	}
	logs.Info("---------------------------")

	// prepared, start first heartbeat
	c.heartbeatChan <- msg

	// keep Heartbeat
	go func() {
		for {
			select {
			case <-c.heartbeatChan:
				// logs.Debug("received heartbeat msg from", conn.GetRemoteAddr())
				time.Sleep(tunnel.HeartbeatInterval)
				resp := tunnel.NewMessage(tunnel.TypeClientHeartbeat, "", c.Name, nil)
				err := conn.SendMessage(resp)
				if err != nil {
					logs.Warn(err.Error())
				}
			case <-time.After(tunnel.HeartbeatTimeout):
				logs.Error("Name [%s], user conn [%s] Heartbeat timeout", c.Name, conn.GetRemoteAddr())
				if conn != nil {
					conn.Close()
				}
			}
		}
	}()
}

func (c *Client) handleBindMsg(msg *tunnel.Message) {
	appProxyName := msg.Content
	if appProxyName == "" {
		return
	}
	appServer, ok := c.onProxyApps[appProxyName]
	if !ok {
		return
	}
	appClient, ok := c.wantProxyApps[appProxyName]
	if !ok {
		return
	}

	localConn, err := tunnel.Dial(appClient.LocalAddress, appClient.LocalPort)
	if err != nil {
		defer localConn.Close()
		logs.Error("Dial local app error.", err)
		return
	}

	remoteConn, err := tunnel.Dial(c.RemoteAddr, appServer.ListenPort)
	if err != nil {
		defer remoteConn.Close()
		logs.Error("Dial remote app error.", err)
		return
	}

	bindMsg := tunnel.NewMessage(tunnel.TypeClientBind, msg.Content, c.Name, nil)
	err = remoteConn.SendMessage(bindMsg)
	if err != nil {
		logs.Error("send join msg to remote conn.", err)
		return
	}

	go tunnel.Bind(localConn, remoteConn)
}
