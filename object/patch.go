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
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/casvisor/casvisor/patch"
)

func installPatch(patchItem *Patch) error {
	output, err := patch.InstallPatch(patchItem.Name)
	if err != nil {
		return err
	}
	if strings.Contains(output, "Failed") {
		patchItem.Status = "Failed"
		patchItem.Message = fmt.Sprintf("update failure: %v", err)
		return fmt.Errorf("update failure: %v", err)
	} else {
		patchItem.Status = "Installed"
	}
	return err
}

func uninstallPatch(patchItem *Patch) error {
	out, err := patch.UninstallPatch(patchItem.Name)
	if err != nil {
		return err
	}
	if strings.Contains(out, "Failed") {
		patchItem.Status = "Failed"
		patchItem.Message = fmt.Sprintf("uninstall failure: %v", err)
		return fmt.Errorf("uninstall failure: %v", err)
	} else {
		patchItem.Status = "Uninstall"
	}
	return nil
}

func getPatches() ([]*Patch, error) {
	patchesOutput, err := patch.GetPatches()
	jsonOutput := strings.ReplaceAll(patchesOutput, "\r\n", "")
	var patches []*Patch
	err = json.Unmarshal([]byte(jsonOutput), &patches)
	if err != nil {
		return nil, err
	}
	var patchesOut []*Patch
	for _, patchItem := range patches {
		if patchItem.Name != "" {
			patchesOut = append(patchesOut, patchItem)
		}
	}
	return patchesOut, nil
}

func mergeTwoPatch(oldpatch *Patch, newPatch *Patch) (*Patch, error) {
	mergedpatch := newPatch
	if mergedpatch.Name == "" {
		mergedpatch.Name = oldpatch.Name
	}
	if mergedpatch.Category == "" {
		mergedpatch.Category = oldpatch.Category
	}
	if mergedpatch.Title == "" {
		mergedpatch.Title = oldpatch.Title
	}
	if mergedpatch.Url == "" {
		mergedpatch.Url = oldpatch.Url
	}
	if mergedpatch.Size == "" {
		mergedpatch.Size = oldpatch.Size
	}
	if mergedpatch.ExpectedStatus == "" {
		mergedpatch.ExpectedStatus = oldpatch.ExpectedStatus
	}
	if mergedpatch.Status == "" {
		mergedpatch.Status = oldpatch.Status
	}
	if mergedpatch.Message == "" {
		mergedpatch.Message = oldpatch.Message
	}
	if mergedpatch.InstallTime == "" {
		mergedpatch.InstallTime = oldpatch.InstallTime
	}
	return mergedpatch, nil
}

func checkHostname(asset *Asset) (bool, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return false, err
	}
	if asset.Name == hostname {
		return true, nil
	}
	return false, nil
}

func UpdatePatches(asset *Asset) error {
	ifHostname, err := checkHostname(asset)
	if err != nil {
		return err
	}
	if !ifHostname {
		return fmt.Errorf("hostname not match")
	}
	patches := asset.Patches
	for _, patchItem := range patches {
		if patchItem.ExpectedStatus == "Install" && patchItem.Status == "Uninstall" {
			err := installPatch(patchItem)
			if err != nil {
				return err
			}
		}
		if patchItem.ExpectedStatus == "Uninstall" && patchItem.Status == "Install" {
			err := uninstallPatch(patchItem)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func RefreshPatches(asset *Asset) error {
	ifHostname, err := checkHostname(asset)
	if err != nil {
		return err
	}
	if !ifHostname {
		return fmt.Errorf("hostname not match")
	}
	patches := asset.Patches
	newPatches, err := getPatches()
	if err != nil {
		return err
	}
	patchMap := make(map[string]*Patch)
	for _, patchItem := range patches {
		patchMap[patchItem.Name] = patchItem
	}

	for _, newPatch := range newPatches {
		oldpatch, ok := patchMap[newPatch.Name]
		if ok {
			mergedpatch, err := mergeTwoPatch(oldpatch, newPatch)
			if err != nil {
				return err
			}
			patchMap[newPatch.Name] = mergedpatch
		} else {
			patchMap[newPatch.Name] = newPatch
		}
	}
	var outputPatches []*Patch
	for _, patchItem := range patchMap {
		outputPatches = append(newPatches, patchItem)
	}
	asset.Patches = outputPatches
	return nil
}
