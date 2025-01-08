// Copyright 2024 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE/2.0
//
// Unless required by law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package service

import (
	"fmt"
	"net/url"

	"github.com/digitalocean/go-libvirt"
)

type MachineKvmClient struct {
	L *libvirt.Libvirt
}

// The URI format is "driver[+transport]://[username @][hostname][:post]/[path][?Extraparameters]", for example "kvm+ssh://root @192.168.1.100/system ".
// Use TCP connection method here: “kvm+tcp://192.168.1.100/system”.
func newMachineKvmClient(username string, hostname string) (MachineKvmClient, error) {
	uri, _ := url.Parse("kvm+tcp://" + hostname + "/system")
	l, err := libvirt.ConnectToURI(uri)
	if err != nil {
		return MachineKvmClient{}, err
	}

	return MachineKvmClient{L: l}, nil
}

func getMachineFromDom(l *libvirt.Libvirt, dom libvirt.Domain) *Machine {
	machine := &Machine{}

	name := dom.Name
	ID := dom.ID
	os, _ := l.DomainGetOsType(dom)
	cpuSize, _ := l.DomainGetMaxVcpus(dom)
	memSize, _ := l.DomainGetMaxMemory(dom)

	machine.Name = name
	machine.Id = fmt.Sprintf("%d", ID)
	machine.Os = os
	machine.CpuSize = fmt.Sprintf("%d", cpuSize)
	machine.MemSize = fmt.Sprintf("%d", memSize)

	return machine
}

func (client MachineKvmClient) GetMachines() ([]*Machine, error) {
	flags := libvirt.ConnectListDomainsActive | libvirt.ConnectListDomainsInactive
	doms, _, err := client.L.ConnectListAllDomains(1, flags)
	if err != nil {
		return nil, err
	}

	machines := []*Machine{}
	for _, dom := range doms {
		machine := getMachineFromDom(client.L, dom)
		machines = append(machines, machine)
	}

	return machines, nil
}

func (client MachineKvmClient) GetMachine(name string) (*Machine, error) {
	dom, err := client.L.DomainLookupByName(name)
	if err != nil {
		return nil, err
	}

	machine := getMachineFromDom(client.L, dom)

	return machine, nil
}
