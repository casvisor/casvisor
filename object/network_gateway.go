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
	"strings"

	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

// NetworkGateway is an SSH jump host in another network, e.g. a data center or an
// office, that Casvisor reaches its assets there through.
type NetworkGateway struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`
	Description string `xorm:"varchar(500)" json:"description"`

	Host     string `xorm:"varchar(200)" json:"host"`
	Port     int    `json:"port"`
	Username string `xorm:"varchar(100)" json:"username"`
	Password string `xorm:"varchar(1000)" json:"password"`

	PrivateKey  string `xorm:"mediumtext" json:"privateKey"`
	Fingerprint string `xorm:"varchar(200)" json:"fingerprint"`
	IsEnabled   bool   `json:"isEnabled"`
}

func GetNetworkGatewayCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&NetworkGateway{})
}

func GetNetworkGateways(owner string) ([]*NetworkGateway, error) {
	gateways := []*NetworkGateway{}
	err := adapter.engine.Desc("created_time").Find(&gateways, &NetworkGateway{Owner: owner})
	if err != nil {
		return gateways, err
	}

	return gateways, nil
}

func GetPaginationNetworkGateways(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*NetworkGateway, error) {
	gateways := []*NetworkGateway{}
	session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	err := session.Find(&gateways)
	if err != nil {
		return gateways, err
	}

	return gateways, nil
}

func getNetworkGateway(owner string, name string) (*NetworkGateway, error) {
	if owner == "" || name == "" {
		return nil, nil
	}

	gateway := NetworkGateway{Owner: owner, Name: name}
	existed, err := adapter.engine.Get(&gateway)
	if err != nil {
		return nil, err
	}

	if existed {
		err = gateway.decryptCredentials()
		if err != nil {
			return nil, err
		}
		return &gateway, nil
	}
	return nil, nil
}

func GetNetworkGateway(id string) (*NetworkGateway, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getNetworkGateway(owner, name)
}

func GetMaskedNetworkGateway(gateway *NetworkGateway, errs ...error) (*NetworkGateway, error) {
	if len(errs) > 0 && errs[0] != nil {
		return nil, errs[0]
	}

	if gateway == nil {
		return nil, nil
	}

	if gateway.Password != "" {
		gateway.Password = "***"
	}
	if gateway.PrivateKey != "" {
		gateway.PrivateKey = "***"
	}
	return gateway, nil
}

func GetMaskedNetworkGateways(gateways []*NetworkGateway, errs ...error) ([]*NetworkGateway, error) {
	if len(errs) > 0 && errs[0] != nil {
		return nil, errs[0]
	}

	for _, gateway := range gateways {
		_, _ = GetMaskedNetworkGateway(gateway)
	}
	return gateways, nil
}

func UpdateNetworkGateway(id string, gateway *NetworkGateway) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	old, err := getNetworkGateway(owner, name)
	if err != nil {
		return false, err
	} else if old == nil {
		return false, nil
	}

	if gateway.Password == "***" {
		gateway.Password = old.Password
	}
	if gateway.PrivateKey == "***" {
		gateway.PrivateKey = old.PrivateKey
	}

	err = gateway.check()
	if err != nil {
		return false, err
	}
	if gateway.Owner != owner {
		return false, fmt.Errorf("the organization of a network gateway cannot be changed")
	}

	err = gateway.encryptCredentials()
	if err != nil {
		return false, err
	}

	gateway.UpdatedTime = util.GetCurrentTime()
	affected, err := adapter.engine.ID(core.PK{owner, name}).AllCols().Update(gateway)
	if err != nil {
		return false, err
	}

	// The assets keep reaching their network through the renamed gateway.
	if affected != 0 && gateway.Name != name {
		_, err = adapter.engine.Where("owner = ? and network_gateway = ?", owner, name).Cols("network_gateway").Update(&Asset{NetworkGateway: gateway.Name})
		if err != nil {
			return false, err
		}
	}

	return affected != 0, nil
}

func AddNetworkGateway(gateway *NetworkGateway) (bool, error) {
	err := gateway.check()
	if err != nil {
		return false, err
	}

	err = gateway.encryptCredentials()
	if err != nil {
		return false, err
	}

	affected, err := adapter.engine.Insert(gateway)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func DeleteNetworkGateway(gateway *NetworkGateway) (bool, error) {
	assets := []*Asset{}
	err := adapter.engine.Cols("name").Where("owner = ? and network_gateway = ?", gateway.Owner, gateway.Name).Find(&assets)
	if err != nil {
		return false, err
	}
	if len(assets) != 0 {
		names := []string{}
		for _, asset := range assets {
			names = append(names, asset.Name)
		}
		return false, fmt.Errorf("the network gateway is still used by the assets: %s", strings.Join(names, ", "))
	}

	affected, err := adapter.engine.ID(core.PK{gateway.Owner, gateway.Name}).Delete(&NetworkGateway{})
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func updateNetworkGatewayFingerprint(id string, fingerprint string) error {
	owner, name := util.GetOwnerAndNameFromId(id)
	_, err := adapter.engine.ID(core.PK{owner, name}).Cols("fingerprint").Update(&NetworkGateway{Fingerprint: fingerprint})
	return err
}

func (gateway *NetworkGateway) GetId() string {
	return util.GetIdFromOwnerAndName(gateway.Owner, gateway.Name)
}

func (gateway *NetworkGateway) check() error {
	if gateway.Name == "" {
		return fmt.Errorf("the name of the network gateway should not be empty")
	}
	if gateway.Host == "" {
		return fmt.Errorf("the host of the network gateway should not be empty")
	}
	if gateway.Port <= 0 || gateway.Port > 65535 {
		return fmt.Errorf("invalid port of the network gateway: %d", gateway.Port)
	}
	if gateway.Username == "" {
		return fmt.Errorf("the username of the network gateway should not be empty")
	}
	return nil
}

func (gateway *NetworkGateway) encryptCredentials() error {
	var err error
	gateway.Password, err = encryptCredential(gateway.Password)
	if err != nil {
		return err
	}
	gateway.PrivateKey, err = encryptCredential(gateway.PrivateKey)
	return err
}

func (gateway *NetworkGateway) decryptCredentials() error {
	var err error
	gateway.Password, err = decryptCredential(gateway.Password)
	if err != nil {
		return err
	}
	gateway.PrivateKey, err = decryptCredential(gateway.PrivateKey)
	return err
}
