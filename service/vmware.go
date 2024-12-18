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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type MachineVMwareClient struct {
	Client          *http.Client
	accessKeyId     string
	accessKeySecret string
	region          string
}

type VirtualMachine struct {
	ID     string `json:"id"`
	Cpu    Cpu    `json:"cpu"`
	Memory int    `json:"memory"`
}

type VirtualMachinePath struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type Cpu struct {
	Processors int `json:"processors"`
}

func NewMachineVMwareClient(accessKeyId string, accessKeySecret string, region string) (MachineVMwareClient, error) {
	client := MachineVMwareClient{
		&http.Client{},
		accessKeyId,
		accessKeySecret, //Here is the credential for VMware Workstation Pro REST service, in the form of {username}:{password}
		region,
	}
	return client, nil
}

func (client MachineVMwareClient) GetMachines() ([]*Machine, error) {
	machines := []*Machine{}
	url := "http://localhost:8697/api/vms"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/vnd.vmware.vmw.rest-v1+json")
	auth := client.accessKeySecret
	encodedAuth := base64.StdEncoding.EncodeToString([]byte(auth))
	req.Header.Add("Authorization", "Basic "+encodedAuth)
	resp, err := client.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var vmList []VirtualMachinePath
	err = json.Unmarshal(body, &vmList)
	if err != nil {
		return nil, err
	}
	if len(vmList) == 0 {
		return nil, nil
	}
	for _, vm := range vmList {
		client.accessKeyId = vm.ID
		name := ExtractFileName(vm.Path)
		machine, err := client.GetMachine(name)
		machine.Region = client.region
		if err != nil {
			return nil, err
		}
		machines = append(machines, machine)
	}
	return machines, nil
}

func (client MachineVMwareClient) GetMachine(name string) (*Machine, error) {
	url := "http://localhost:8697/api/vms/" + client.accessKeyId
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Accept", "application/vnd.vmware.vmw.rest-v1+json")
	auth := client.accessKeySecret
	encodedAuth := base64.StdEncoding.EncodeToString([]byte(auth))
	req.Header.Add("Authorization", "Basic "+encodedAuth)
	resp, err := client.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var vm VirtualMachine
	err = json.Unmarshal(body, &vm)
	if err != nil {
		return nil, err
	}
	var machine *Machine
	machine = &Machine{
		Name:    name,
		Id:      vm.ID,
		MemSize: fmt.Sprintf("%d", vm.Memory),
		CpuSize: fmt.Sprintf("%d", vm.Cpu.Processors),
	}

	return machine, nil
}
