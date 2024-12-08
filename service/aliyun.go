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

	"github.com/aliyun/alibaba-cloud-sdk-go/services/ecs"
)

type Machine struct {
	Name        string `xorm:"varchar(100)" json:"name"`
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

type MachineAliyunClient struct {
	Client *ecs.Client
}

func NewMachineAliyunClient(accessKeyId string, accessKeySecret string, region string) (*MachineAliyunClient, error) {
	client, err := ecs.NewClientWithAccessKey(
		region,
		accessKeyId,
		accessKeySecret,
	)
	if err != nil {
		return nil, err
	}

	return &MachineAliyunClient{Client: client}, nil
}

func (client MachineAliyunClient) getMachines() ([]*Machine, error) {
	request := ecs.CreateDescribeInstancesRequest()
	request.PageSize = "100"

	response, err := client.Client.DescribeInstances(request)
	if err != nil {
		return nil, err
	}

	var machines []*Machine
	for _, instance := range response.Instances.Instance {
		machine := &Machine{
			Name:        instance.InstanceName,
			DisplayName: instance.InstanceId,
			CreatedTime: instance.CreationTime,
			ExpireTime:  instance.ExpiredTime,
			Region:      instance.RegionId,
			Zone:        instance.ZoneId,
			Type:        instance.InstanceType,
			Size:        fmt.Sprintf("%d", instance.Cpu) + "C" + fmt.Sprintf("%d", instance.Memory) + "G",
			State:       instance.Status,
			Category:    instance.InstanceType + "." + instance.InstanceTypeFamily,
			Image:       instance.ImageId,
			Os:          instance.OSName,
			PublicIp:    instance.PublicIpAddress.IpAddress[0],
			PrivateIp:   instance.VpcAttributes.PrivateIpAddress.IpAddress[0],
			CpuSize:     fmt.Sprintf("%d", instance.Cpu),
			MemSize:     fmt.Sprintf("%d", instance.Memory),
		}
		machines = append(machines, machine)
	}

	return machines, nil
}
