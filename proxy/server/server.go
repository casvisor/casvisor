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

package server

import (
	"errors"
	"github.com/casvisor/casvisor/object"
	"github.com/casvisor/casvisor/proxy/tunnel"
	"io"
	"sync"
	"time"

	"github.com/beego/beego/logs"
)

type ServerStatus int

const (
	Idle ServerStatus = iota
	Ready
	Work
)

// commonServer: Used to establish connection and keep heartbeat。
// appServer(appProxyServer): Used to proxy data
type Server struct {
	Name string

	listenPort    int
	listener      *tunnel.Listener
	status        ServerStatus         // used in appServer only
	heartbeatChan chan *tunnel.Message // when get heartbeat msg, put msg in, used in commonServer only

	wantProxyApps map[string]*tunnel.AppInfo
	onProxyApps   map[string]*Server // appServer which is listening its own port, used in commonServer only
	userConnMap   sync.Map           // map[appServerName]UserConn, used in appServer only
}

func NewProxyServer(name string, listenPort int) (*Server, error) {
	listener, err := tunnel.NewListener(listenPort)
	if err != nil {
		return nil, err
	}

	server := &Server{
		Name:          name,
		listenPort:    listenPort,
		status:        Idle,
		listener:      listener,
		onProxyApps:   make(map[string]*Server),
		heartbeatChan: make(chan *tunnel.Message, 1),
	}
	return server, nil
}

func (s *Server) SetStatus(status ServerStatus) {
	s.status = status
}

func (s *Server) GetStatus() ServerStatus {
	return s.status
}

func (s *Server) SetWantProxyApps(apps []*tunnel.AppInfo) {
	s.wantProxyApps = make(map[string]*tunnel.AppInfo, len(apps))
	for _, app := range apps {
		s.wantProxyApps[app.Name] = app
	}
}

func (s *Server) CloseClient(clientConn *tunnel.Conn) {
	logs.Info("close conn: ", clientConn.String())
	clientConn.Close()
	for _, app := range s.onProxyApps {
		app.listener.Close()
	}

	s.onProxyApps = make(map[string]*Server, len(s.onProxyApps))
}

func (s *Server) checkApp(msg *tunnel.Message) (map[string]*tunnel.AppInfo, error) {
	if msg.Meta == nil {
		return nil, errors.New("has no app to proxy")
	}

	wantProxyApps := make(map[string]*tunnel.AppInfo)
	for name, app := range msg.Meta.(map[string]interface{}) {
		App := app.(map[string]interface{})
		wantProxyApps[name] = &tunnel.AppInfo{
			Name:      App["Name"].(string),
			LocalPort: int(App["LocalPort"].(float64)),
		}
	}

	waitToProxyAppsInfo := make(map[string]*tunnel.AppInfo)
	for _, appInfo := range wantProxyApps {
		port, err := tunnel.TryGetFreePort(tunnel.RetryTimes)
		if err != nil {
			return nil, err
		}

		appInfo.ListenPort = port
		waitToProxyAppsInfo[appInfo.Name] = appInfo

		asset, err := object.GetAsset(appInfo.Name)
		if err != nil {
			return nil, err
		}
		asset.RemotePort = port
		_, err = object.UpdateAsset(appInfo.Name, asset)
		if err != nil {
			return nil, err
		}
	}
	return waitToProxyAppsInfo, nil
}

func (s *Server) initApp(clientConn *tunnel.Conn, msg *tunnel.Message) {
	waitToProxyAppsInfo, err := s.checkApp(msg)
	if err != nil {
		logs.Error(err)
		s.CloseClient(clientConn)
		return
	}

	for _, app := range waitToProxyAppsInfo {
		go s.startProxyApp(clientConn, app)
	}

	// send app info to client
	resp := tunnel.NewMessage(tunnel.TypeAppMsg, "", s.Name, waitToProxyAppsInfo)
	err = clientConn.SendMessage(resp)
	if err != nil {
		logs.Error(err.Error())
		s.CloseClient(clientConn)
		return
	}

	// keep Heartbeat
	go func() {
		for {
			select {
			case <-s.heartbeatChan:
				// logs.Debug("received heartbeat msg from", clientConn.GetRemoteAddr())
				resp := tunnel.NewMessage(tunnel.TypeServerHeartbeat, "", s.Name, nil)
				err := clientConn.SendMessage(resp)
				if err != nil {
					logs.Warn(err.Error())
					return
				}
			case <-time.After(tunnel.HeartbeatTimeout):
				logs.Error("Name [%s], user conn [%s] Heartbeat timeout", s.Name, clientConn.GetRemoteAddr())
				if clientConn != nil {
					s.CloseClient(clientConn)
				}
				return
			}
		}
	}()
}

func (s *Server) startProxyApp(clientConn *tunnel.Conn, app *tunnel.AppInfo) {
	if ps, ok := s.onProxyApps[app.Name]; ok {
		ps.listener.Close()
	}

	appProxyServer, err := NewProxyServer(app.Name, app.ListenPort)
	if err != nil {
		logs.Error(err.Error())
		return
	}
	s.onProxyApps[app.Name] = appProxyServer

	for {
		conn, err := appProxyServer.listener.GetConn()
		if err != nil {
			logs.Error(err)
			return
		}
		logs.Info("user connect success:", conn.String())

		// connection from client
		if appProxyServer.GetStatus() == Ready && conn.GetRemoteIP() == clientConn.GetRemoteIP() {
			msg, err := conn.ReadMessage()
			if err != nil {
				logs.Warn("proxy client read err:", err.Error())
				if err == io.EOF {
					logs.Error("Name [%s], server is dead!", appProxyServer.Name)
					s.CloseClient(conn)
					return
				}
				continue
			}
			if msg.Type != tunnel.TypeClientBind {
				logs.Warn("get wrong msg")
				continue
			}

			appProxyPort := msg.Content
			newClientConn, ok := s.userConnMap.Load(appProxyPort)
			if !ok {
				logs.Error("userConnMap load failed. appProxyAddrEny:", appProxyPort)
				continue
			}
			s.userConnMap.Delete(appProxyPort)

			waitToJoinClientConn := conn
			waitToJoinUserConn := newClientConn.(*tunnel.Conn)
			go tunnel.Bind(waitToJoinUserConn, waitToJoinClientConn)
			appProxyServer.SetStatus(Work)
		} else {
			// connection from user
			s.userConnMap.Store(app.Name, conn)
			time.AfterFunc(tunnel.BindConnTimeout, func() {
				uc, ok := s.userConnMap.Load(app.Name)
				if !ok || uc == nil {
					return
				}
				if conn == uc.(*tunnel.Conn) {
					logs.Error("Name [%s], user conn [%s], join connections timeout", s.Name, conn.GetRemoteAddr())
					conn.Close()
				}
				appProxyServer.SetStatus(Idle)
			})

			// notify client to connect
			msg := tunnel.NewMessage(tunnel.TypeAppWaitBind, app.Name, app.Name, nil)
			err := clientConn.SendMessage(msg)
			if err != nil {
				logs.Error(err)
				return
			}
			appProxyServer.SetStatus(Ready)
		}
	}
}

func (s *Server) Serve() {
	if s == nil {
		logs.Error("proxy server is nil")
		return
	}
	if s.listener == nil {
		logs.Error("listener is nil")
		return
	}
	for {
		clientConn, err := s.listener.GetConn()
		if err != nil {
			logs.Warn("proxy get conn err:", err.Error())
			continue
		}
		go s.process(clientConn)
	}
}

func (s *Server) process(clientConn *tunnel.Conn) {
	for {
		msg, err := clientConn.ReadMessage()
		if err != nil {
			logs.Error(err.Error())
			if err == io.EOF {
				logs.Info("Name [%s], client is dead!", s.Name)
				s.CloseClient(clientConn)
			}
			return
		}

		switch msg.Type {
		case tunnel.TypeInitApp:
			go s.initApp(clientConn, msg)
		case tunnel.TypeClientHeartbeat:
			s.heartbeatChan <- msg
		}
	}
}
