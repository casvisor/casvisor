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

import "fmt"

type MachineClientInterface interface {
	GetMachines() ([]*Machine, error)
	GetMachine(name string) (*Machine, error)
	UpdateMachineState(name string, state string) (bool, string, error)
}

func NewMachineClient(providerType string, accessKeyId string, accessKeySecret string, region string) (MachineClientInterface, error) {
	var res MachineClientInterface
	var err error
	if providerType == "Aliyun" {
		res, err = newMachineAliyunClient(accessKeyId, accessKeySecret, region)
	} else if providerType == "Azure" {
		res, err = newMachineAzureClient(accessKeyId, accessKeySecret)
	} else if providerType == "VMware" {
		res, err = newMachineVmwareClient(accessKeyId, accessKeySecret)
	} else if providerType == "KVM" {
		res, err = newMachineKvmClient(accessKeyId, accessKeySecret)
	} else {
		return nil, fmt.Errorf("unsupported provider type: %s", providerType)
	}

	if err != nil {
		return nil, err
	}

	return res, nil
}
