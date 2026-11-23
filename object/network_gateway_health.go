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
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/beego/beego/logs"
	"github.com/casvisor/casvisor/conf"
	"github.com/casvisor/casvisor/util"
)

const (
	GatewayHealthy   = "Healthy"
	GatewayUnhealthy = "Unhealthy"

	defaultGatewayCheckInterval = 60
	gatewayCheckConcurrency     = 10
)

type gatewayHealth struct {
	status        string
	checkTime     string
	latency       int64
	lastError     string
	lastErrorTime string
}

var (
	gatewayHealths   = map[string]*gatewayHealth{}
	gatewayHealthsMu sync.Mutex
)

// gatewayUnreachableError is a gateway failing itself, rather than failing to reach the asset,
// so that the next gateway of the domain is tried.
type gatewayUnreachableError struct {
	err error
}

func (e *gatewayUnreachableError) Error() string {
	return e.err.Error()
}

func (e *gatewayUnreachableError) Unwrap() error {
	return e.err
}

func updateGatewayHealth(id string, update func(health *gatewayHealth)) {
	gatewayHealthsMu.Lock()
	defer gatewayHealthsMu.Unlock()
	health := gatewayHealths[id]
	if health == nil {
		health = &gatewayHealth{}
		gatewayHealths[id] = health
	}
	update(health)
}

func getGatewayHealth(id string) gatewayHealth {
	gatewayHealthsMu.Lock()
	defer gatewayHealthsMu.Unlock()
	if health := gatewayHealths[id]; health != nil {
		return *health
	}
	return gatewayHealth{}
}

func removeGatewayHealth(id string) {
	gatewayHealthsMu.Lock()
	defer gatewayHealthsMu.Unlock()
	delete(gatewayHealths, id)
}

func (gateway *NetworkGateway) fillHealth() {
	health := getGatewayHealth(gateway.GetId())
	gateway.HealthStatus = health.status
	gateway.HealthCheckTime = health.checkTime
	gateway.Latency = health.latency
	gateway.LastError = health.lastError
	gateway.LastErrorTime = health.lastErrorTime
}

// recordGatewayDial keeps how a connection through the gateway went: a gateway that could not be
// reached is unhealthy until it is reached again.
func recordGatewayDial(gateway *NetworkGateway, err error) {
	updateGatewayHealth(gateway.GetId(), func(health *gatewayHealth) {
		var unreachable *gatewayUnreachableError
		if err == nil {
			health.status = GatewayHealthy
			return
		}
		if errors.As(err, &unreachable) {
			health.status = GatewayUnhealthy
		}
		health.lastError = truncate(err.Error(), 1000)
		health.lastErrorTime = util.GetCurrentTime()
	})
}

// getDomainGateways returns the enabled gateways of the domain in the order they are tried: the
// unhealthy ones last, then by priority, a smaller number first, and in random order by weight
// among the gateways of the same priority.
func getDomainGateways(owner string, domain string) ([]*NetworkGateway, error) {
	gateways := []*NetworkGateway{}
	err := adapter.engine.Where("owner = ? and domain = ? and is_enabled = ?", owner, domain, true).Find(&gateways)
	if err != nil {
		return nil, err
	}

	keys := map[*NetworkGateway]float64{}
	unhealthy := map[*NetworkGateway]bool{}
	for _, gateway := range gateways {
		err = gateway.decryptCredentials()
		if err != nil {
			return nil, err
		}
		weight := max(gateway.Weight, 1)
		keys[gateway] = math.Pow(rand.Float64(), 1/float64(weight))
		unhealthy[gateway] = getGatewayHealth(gateway.GetId()).status == GatewayUnhealthy
	}

	sort.SliceStable(gateways, func(i, j int) bool {
		a, b := gateways[i], gateways[j]
		if unhealthy[a] != unhealthy[b] {
			return !unhealthy[a]
		}
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		return keys[a] > keys[b]
	})
	return gateways, nil
}

func (gateway *NetworkGateway) dial(address string) (net.Conn, error) {
	if gateway.IsAgent() {
		return gateway.dialAgent(address)
	}
	return gateway.Dial(address)
}

// dialDomain connects to address through the gateways of the domain of gateway, the next one
// when a gateway can't be reached.
func dialDomain(asset *Asset, gateway *NetworkGateway, address string) (net.Conn, error) {
	gateways, err := getDomainGateways(gateway.Owner, gateway.Domain)
	if err != nil {
		return nil, err
	}
	if len(gateways) == 0 {
		return nil, fmt.Errorf("the network domain: %s of the asset: %s has no enabled network gateway", gateway.Domain, asset.GetId())
	}

	errs := []string{}
	for _, g := range gateways {
		conn, err := g.dial(address)
		recordGatewayDial(g, err)
		if err == nil {
			return conn, nil
		}
		var unreachable *gatewayUnreachableError
		if !errors.As(err, &unreachable) {
			return nil, err
		}
		errs = append(errs, err.Error())
	}
	return nil, fmt.Errorf("no network gateway of the network domain: %s could be reached: %s", gateway.Domain, strings.Join(errs, "; "))
}

// CheckNetworkGateway connects to an SSH jump host, or checks that the agent is online, and
// returns the version of its SSH server or of the agent.
func CheckNetworkGateway(gateway *NetworkGateway) (string, error) {
	start := time.Now()
	var version string
	var err error
	if gateway.IsAgent() {
		version, err = PingAgent(gateway)
	} else {
		client, dialErr := gateway.DialSsh()
		err = dialErr
		if err == nil {
			version = string(client.ServerVersion())
			_ = client.Close()
		}
	}

	now := util.GetCurrentTime()
	updateGatewayHealth(gateway.GetId(), func(health *gatewayHealth) {
		health.checkTime = now
		if err == nil {
			health.status = GatewayHealthy
			health.latency = time.Since(start).Milliseconds()
			return
		}
		health.status = GatewayUnhealthy
		health.latency = 0
		health.lastError = truncate(err.Error(), 1000)
		health.lastErrorTime = now
	})
	return version, err
}

func checkGateways() {
	gateways := []*NetworkGateway{}
	err := adapter.engine.Where("is_enabled = ?", true).Find(&gateways)
	if err != nil {
		logs.Error(fmt.Sprintf("checkGateways(): %s", err.Error()))
		return
	}

	limit := make(chan struct{}, gatewayCheckConcurrency)
	var wg sync.WaitGroup
	for _, gateway := range gateways {
		err = gateway.decryptCredentials()
		if err != nil {
			continue
		}
		wg.Add(1)
		limit <- struct{}{}
		go func(gateway *NetworkGateway) {
			defer wg.Done()
			defer func() { <-limit }()
			_, _ = CheckNetworkGateway(gateway)
		}(gateway)
	}
	wg.Wait()
}

// StartNetworkGatewayChecker checks every enabled network gateway every networkGatewayCheckInterval seconds, 60 by default, 0 to never check.
func StartNetworkGatewayChecker() {
	interval := defaultGatewayCheckInterval
	if value := conf.GetConfigString("networkGatewayCheckInterval"); value != "" {
		interval = util.ParseInt(value)
	}
	if interval <= 0 {
		return
	}

	go func() {
		// the agents get a moment to connect again after a restart
		time.Sleep(10 * time.Second)
		for {
			checkGateways()
			time.Sleep(time.Duration(interval) * time.Second)
		}
	}()
}
