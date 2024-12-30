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

func getActiveCloudProviders(owner string) ([]*Provider, error) {
	providers, err := GetProviders(owner)
	if err != nil {
		return nil, err
	}

	res := []*Provider{}
	for _, provider := range providers {
		if provider.ClientId != "" && provider.ClientSecret != "" && (provider.Category == "Public Cloud" || provider.Category == "Private Cloud") && provider.State == "Active" {
			res = append(res, provider)
		}
	}
	return res, nil
}

func getActiveBlockchainProvider(owner string) (*Provider, error) {
	providers, err := GetProviders(owner)
	if err != nil {
		return nil, err
	}

	for _, provider := range providers {
		if provider.ClientId != "" && provider.ClientSecret != "" && provider.Category == "Blockchain" && provider.State == "Active" {
			return provider, nil
		}
	}
	return nil, nil
}
