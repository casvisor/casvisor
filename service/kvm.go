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

package service

import (
	"fmt"
	"github.com/libvirt/libvirt-go"
)

// KvmUri is its uniform resource identifier, such as "qemu:///system"
type MachineKvmClient struct {
	kvmUri string
}

func newMachineKvmClient(kvmUri string) (MachineKvmClient, error) {
	client := MachineKvmClient{
		kvmUri,
	}
	return client, nil
}

func getMachineFromDom(dom libvirt.Domain) *Machine {
	machine := &Machine{}

	name, _ := dom.GetName()
	ID, _ := dom.GetID()
	os, _ := dom.GetOSType()
	cpuSize, _ := dom.GetMaxVcpus()
	memSize, _ := dom.GetMaxMemory()

	machine.Name = name
	machine.Id = fmt.Sprintf("%d", ID)
	machine.Os = os
	machine.CpuSize = fmt.Sprintf("%d", cpuSize)
	machine.MemSize = fmt.Sprintf("%d", memSize)

	return machine
}

func (client MachineKvmClient) GetMachines() ([]*Machine, error) {
	conn, err := libvirt.NewConnect(client.kvmUri)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	doms, err := conn.ListAllDomains(libvirt.CONNECT_LIST_DOMAINS_ACTIVE)
	if err != nil {
		return nil, err
	}

	machines := []*Machine{}
	for _, dom := range doms {
		machine := getMachineFromDom(dom)
		machines = append(machines, machine)
	}

	return machines, nil
}

func (client MachineKvmClient) GetMachine(name string) (*Machine, error) {
	conn, err := libvirt.NewConnect(client.kvmUri)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	dom, err := conn.LookupDomainByName(name)
	if err != nil {
		return nil, err
	}

	machine := getMachineFromDom(*dom)

	return machine, nil
}
