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

package object

import (
	"fmt"

	"github.com/casvisor/casvisor/service"
	"github.com/casvisor/casvisor/util"
	"xorm.io/core"
)

type Machine struct {
	Owner       string `xorm:"varchar(100) notnull pk" json:"owner"`
	Name        string `xorm:"varchar(100) notnull pk" json:"name"`
	Id          string `xorm:"varchar(100)" json:"id"`
	Provider    string `xorm:"varchar(100)" json:"provider"`
	CreatedTime string `xorm:"varchar(100)" json:"createdTime"`
	UpdatedTime string `xorm:"varchar(100)" json:"updatedTime"`
	ExpireTime  string `xorm:"varchar(100)" json:"expireTime"`
	DisplayName string `xorm:"varchar(100)" json:"displayName"`

	Region   string `xorm:"varchar(100)" json:"region"`
	Zone     string `xorm:"varchar(100)" json:"zone"`
	Category string `xorm:"varchar(100)" json:"category"`
	Type     string `xorm:"varchar(100)" json:"type"`
	Size     string `xorm:"varchar(100)" json:"size"`
	Tag      string `xorm:"varchar(100)" json:"tag"`
	State    string `xorm:"varchar(100)" json:"state"`

	Image     string `xorm:"varchar(100)" json:"image"`
	Os        string `xorm:"varchar(100)" json:"os"`
	PublicIp  string `xorm:"varchar(100)" json:"publicIp"`
	PrivateIp string `xorm:"varchar(100)" json:"privateIp"`
	CpuSize   string `xorm:"varchar(100)" json:"cpuSize"`
	MemSize   string `xorm:"varchar(100)" json:"memSize"`
}

func getMachineFromService(owner string, provider string, clientMachine *service.Machine) *Machine {
	return &Machine{
		Owner:       owner,
		Name:        clientMachine.Name,
		Id:          clientMachine.Id,
		Provider:    provider,
		CreatedTime: clientMachine.CreatedTime,
		UpdatedTime: clientMachine.UpdatedTime,
		ExpireTime:  clientMachine.ExpireTime,
		DisplayName: clientMachine.DisplayName,
		Region:      clientMachine.Region,
		Zone:        clientMachine.Zone,
		Category:    clientMachine.Category,
		Type:        clientMachine.Type,
		Size:        clientMachine.Size,
		Tag:         clientMachine.Tag,
		State:       clientMachine.State,
		Image:       clientMachine.Image,
		Os:          clientMachine.Os,
		PublicIp:    clientMachine.PublicIp,
		PrivateIp:   clientMachine.PrivateIp,
		CpuSize:     clientMachine.CpuSize,
		MemSize:     clientMachine.MemSize,
	}
}

func GetMachineCount(owner, field, value string) (int64, error) {
	session := GetSession(owner, -1, -1, field, value, "", "")
	return session.Count(&Machine{})
}

func GetMachines(owner string) ([]*Machine, error) {
	machines := []*Machine{}
	providers, err := GetProviders(owner)
	if err != nil {
		return machines, err
	}

	for _, provider := range providers {
		if provider.ClientId == "" || provider.ClientSecret == "" {
			continue
		}

		client, err2 := service.NewMachineClient(provider.Type, provider.ClientId, provider.ClientSecret, provider.Region)
		if err2 != nil || client == nil {
			return machines, err2
		}
		clientMachines, err2 := (*client).GetMachines()
		if err2 != nil {
			return machines, err2
		}

		for _, clientMachine := range clientMachines {
			machine := getMachineFromService(owner, provider.Name, clientMachine)
			machines = append(machines, machine)
		}
	}

	return machines, nil
}

func GetPaginationMachines(owner string, offset, limit int, field, value, sortField, sortOrder string) ([]*Machine, error) {
	return GetMachines(owner)

	// machines := []*Machine{}
	// session := GetSession(owner, offset, limit, field, value, sortField, sortOrder)
	// err := session.Find(&machines)
	// if err != nil {
	//	return machines, err
	// }
	//
	// return machines, nil
}

func getMachine(owner string, name string) (*Machine, error) {
	providers, err := GetProviders(owner)
	if err != nil {
		return nil, err
	}

	for _, provider := range providers {
		client, err2 := service.NewMachineClient(provider.Type, provider.ClientId, provider.ClientSecret, provider.Region)
		if err2 != nil || client == nil {
			return nil, err2
		}

		clientMachine, err2 := (*client).GetMachine(name)
		if err2 != nil {
			return nil, err2
		}
		if clientMachine == nil {
			continue
		}
		machine := getMachineFromService(owner, provider.Name, clientMachine)
		return machine, nil
	}

	return nil, nil
}

func GetMachine(id string) (*Machine, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	return getMachine(owner, name)
}

func GetMaskedMachine(machine *Machine, errs ...error) (*Machine, error) {
	if len(errs) > 0 && errs[0] != nil {
		return nil, errs[0]
	}

	if machine == nil {
		return nil, nil
	}

	// if machine.ClientSecret != "" {
	//	machine.ClientSecret = "***"
	// }
	return machine, nil
}

func GetMaskedMachines(machines []*Machine, errs ...error) ([]*Machine, error) {
	if len(errs) > 0 && errs[0] != nil {
		return nil, errs[0]
	}

	var err error
	for _, machine := range machines {
		machine, err = GetMaskedMachine(machine)
		if err != nil {
			return nil, err
		}
	}

	return machines, nil
}

func UpdateMachine(id string, machine *Machine) (bool, error) {
	owner, name := util.GetOwnerAndNameFromId(id)
	p, err := getMachine(owner, name)
	if err != nil {
		return false, err
	} else if p == nil {
		return false, nil
	}

	// if machine.ClientSecret == "***" {
	//	machine.ClientSecret = p.ClientSecret
	// }

	affected, err := adapter.engine.ID(core.PK{owner, name}).AllCols().Update(machine)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func AddMachine(machine *Machine) (bool, error) {
	affected, err := adapter.engine.Insert(machine)
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func DeleteMachine(machine *Machine) (bool, error) {
	affected, err := adapter.engine.ID(core.PK{machine.Owner, machine.Name}).Delete(&Machine{})
	if err != nil {
		return false, err
	}

	return affected != 0, nil
}

func (machine *Machine) getId() string {
	return fmt.Sprintf("%s/%s", machine.Owner, machine.Name)
}
