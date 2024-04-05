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
	"errors"
	"io"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/proxy/tunnel"
)

type Client struct {
	Name          string
	LocalPort     int
	RemoteAddr    string
	RemotePort    int
	wantProxyApps map[string]*tunnel.AppInfo
	onProxyApps   map[string]*tunnel.AppInfo
	heartbeatChan chan *tunnel.Message // when get heartbeat msg, put msg in
	conn          *tunnel.Conn
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

func (c *Client) sendInitAppMsg() error {
	if c.wantProxyApps == nil {
		return errors.New("wantProxyApps is nil")
	}

	msg := tunnel.NewMessage(tunnel.InitApp, "", c.Name, c.wantProxyApps)
	if err := c.conn.SendMessage(msg); err != nil {
		return err
	}
	return nil
}

func (c *Client) storeServerApp(msg *tunnel.Message) {
	if msg.Meta == nil {
		panic("server app info is nil")
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
		logs.Info("[%s]:\t%s:%d", name, c.conn.GetRemoteIP(), app.ListenPort)
	}
	logs.Info("---------------------------")

	// prepared, start first heartbeat
	c.heartbeatChan <- msg

	// keep Heartbeat
	go func() {
		for {
			select {
			case <-c.heartbeatChan:
				time.Sleep(tunnel.HeartbeatInterval)
				resp := tunnel.NewMessage(tunnel.ClientHeartbeat, "", c.Name, nil)
				err := c.conn.SendMessage(resp)
				if err != nil {
					return
				}
			case <-time.After(tunnel.HeartbeatTimeout):
				if c.conn != nil {
					c.conn.Close()
					return
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
		panic(err)
		return
	}

	remoteConn, err := tunnel.Dial(c.RemoteAddr, appServer.ListenPort)
	if err != nil {
		defer remoteConn.Close()
		panic(err)
		return
	}

	bindMsg := tunnel.NewMessage(tunnel.ClientBind, msg.Content, c.Name, nil)
	err = remoteConn.SendMessage(bindMsg)
	if err != nil {
		panic(err)
		return
	}

	go tunnel.Bind(localConn, remoteConn)
}

func (c *Client) Run() {
	conn, err := tunnel.Dial(c.RemoteAddr, c.RemotePort)
	if err != nil {
		logs.Error("Dial to server error.", err)
		return
	}
	c.conn = conn

	err = c.sendInitAppMsg()
	if err != nil {
		return
	}

	for {
		msg, err := c.conn.ReadMessage()
		if err != nil {
			if err == io.EOF {
				logs.Info("Name [%s], client is dead!", c.Name)
				return
			}
			logs.Error("Read message from server error.", err)
			break
		}

		switch msg.Type {
		case tunnel.ServerHeartbeat:
			c.heartbeatChan <- msg
		case tunnel.AppMsg:
			c.storeServerApp(msg)
		case tunnel.AppWaitBind:
			go c.handleBindMsg(msg)
		}
	}
}
