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
	"bytes"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/casvisor/casvisor/util"
)

const (
	CloudProviderAliyun  = "Aliyun"
	CloudProviderAws     = "AWS"
	CloudProviderTencent = "Tencent"
	CloudProviderAzure   = "Azure"
	CloudProviderGcp     = "GCP"
)

var cloudProviders = []string{CloudProviderAliyun, CloudProviderAws, CloudProviderTencent, CloudProviderAzure, CloudProviderGcp}

// CloudInstance is a server of a cloud account, as its provider lists it.
type CloudInstance struct {
	Id         string   `json:"id"`
	Name       string   `json:"name"`
	Region     string   `json:"region"`
	Type       string   `json:"type"`
	Status     string   `json:"status"`
	Os         string   `json:"os"`
	PrivateIps []string `json:"privateIps"`
	PublicIps  []string `json:"publicIps"`
}

const cloudRegionConcurrency = 8

var cloudHttpClient = &http.Client{
	Timeout:   30 * time.Second,
	Transport: &http.Transport{Proxy: http.ProxyFromEnvironment},
}

func readCloudResponse(req *http.Request) (int, []byte, error) {
	resp, err := cloudHttpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return resp.StatusCode, body, err
}

func hmacSha256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// percentEncode encodes as RFC 3986 asks, which the signatures of Alibaba Cloud and AWS go by.
func percentEncode(s string) string {
	s = url.QueryEscape(s)
	s = strings.ReplaceAll(s, "+", "%20")
	s = strings.ReplaceAll(s, "*", "%2A")
	return strings.ReplaceAll(s, "%7E", "~")
}

func getCanonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for key := range params {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, percentEncode(key)+"="+percentEncode(params[key]))
	}
	return strings.Join(parts, "&")
}

func getOsFromName(name string) string {
	if strings.Contains(strings.ToLower(name), "windows") {
		return "Windows"
	}
	return "Linux"
}

// listCloudInstances lists the servers of the account in its regions, or in all of them when
// it names none. Any region failing fails the whole list, so that a sync never takes the
// servers of a region it could not read for gone.
func listCloudInstances(account *CloudAccount) ([]*CloudInstance, error) {
	// Azure and GCP list the servers of all regions at once
	if account.Provider == CloudProviderAzure || account.Provider == CloudProviderGcp {
		var instances []*CloudInstance
		var err error
		if account.Provider == CloudProviderAzure {
			instances, err = listAzureInstances(account)
		} else {
			instances, err = listGcpInstances(account)
		}
		if err != nil {
			return nil, err
		}

		res := []*CloudInstance{}
		for _, instance := range instances {
			if len(account.Regions) == 0 || slices.Contains(account.Regions, instance.Region) {
				res = append(res, instance)
			}
		}
		sortCloudInstances(res)
		return res, nil
	}

	regions := account.Regions
	if len(regions) == 0 {
		var err error
		regions, err = listCloudRegions(account)
		if err != nil {
			return nil, err
		}
	}

	var mu sync.Mutex
	var firstErr error
	res := []*CloudInstance{}
	limit := make(chan struct{}, cloudRegionConcurrency)
	var wg sync.WaitGroup
	for _, region := range regions {
		wg.Add(1)
		limit <- struct{}{}
		go func(region string) {
			defer wg.Done()
			defer func() { <-limit }()

			var instances []*CloudInstance
			var err error
			switch account.Provider {
			case CloudProviderAliyun:
				instances, err = listAliyunInstances(account, region)
			case CloudProviderAws:
				instances, err = listAwsInstances(account, region)
			case CloudProviderTencent:
				instances, err = listTencentInstances(account, region)
			default:
				err = fmt.Errorf("unknown cloud provider: %s", account.Provider)
			}

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("region %s: %s", region, err.Error())
				}
				return
			}
			res = append(res, instances...)
		}(region)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}

	sortCloudInstances(res)
	return res, nil
}

func sortCloudInstances(instances []*CloudInstance) {
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].Region != instances[j].Region {
			return instances[i].Region < instances[j].Region
		}
		return instances[i].Id < instances[j].Id
	})
}

// listCloudRegions lists the regions the account can use.
func listCloudRegions(account *CloudAccount) ([]string, error) {
	switch account.Provider {
	case CloudProviderAliyun:
		return listAliyunRegions(account)
	case CloudProviderAws:
		return listAwsRegions(account)
	case CloudProviderTencent:
		return listTencentRegions(account)
	case CloudProviderAzure:
		return listAzureRegions(account)
	case CloudProviderGcp:
		return listGcpRegions(account)
	}
	return nil, fmt.Errorf("unknown cloud provider: %s", account.Provider)
}

// Alibaba Cloud ECS, with the RPC signature of version 1.0.

func aliyunCall(account *CloudAccount, endpoint string, action string, params map[string]string, v interface{}) error {
	query := map[string]string{
		"Format":           "JSON",
		"Version":          "2014-05-26",
		"AccessKeyId":      account.AccessKey,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   util.GenerateId(),
		"Timestamp":        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		"Action":           action,
	}
	for key, value := range params {
		query[key] = value
	}

	canonical := getCanonicalQuery(query)
	mac := hmac.New(sha1.New, []byte(account.AccessSecret+"&"))
	mac.Write([]byte("GET&%2F&" + percentEncode(canonical)))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequest(http.MethodGet, "https://"+endpoint+"/?"+canonical+"&Signature="+percentEncode(signature), nil)
	if err != nil {
		return err
	}
	status, body, err := readCloudResponse(req)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		var apiErr struct {
			Code    string
			Message string
		}
		if json.Unmarshal(body, &apiErr) == nil && apiErr.Code != "" {
			return fmt.Errorf("%s: %s", apiErr.Code, apiErr.Message)
		}
		return fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, v)
}

func listAliyunRegions(account *CloudAccount) ([]string, error) {
	var resp struct {
		Regions struct {
			Region []struct {
				RegionId string
			}
		}
	}
	err := aliyunCall(account, "ecs.aliyuncs.com", "DescribeRegions", map[string]string{}, &resp)
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, region := range resp.Regions.Region {
		res = append(res, region.RegionId)
	}
	sort.Strings(res)
	return res, nil
}

func listAliyunInstances(account *CloudAccount, region string) ([]*CloudInstance, error) {
	res := []*CloudInstance{}
	for page := 1; ; page++ {
		var resp struct {
			TotalCount int
			Instances  struct {
				Instance []struct {
					InstanceId      string
					InstanceName    string
					InstanceType    string
					Status          string
					OSType          string
					OSName          string
					PublicIpAddress struct {
						IpAddress []string
					}
					EipAddress struct {
						IpAddress string
					}
					VpcAttributes struct {
						PrivateIpAddress struct {
							IpAddress []string
						}
					}
					InnerIpAddress struct {
						IpAddress []string
					}
				}
			}
		}
		err := aliyunCall(account, "ecs."+region+".aliyuncs.com", "DescribeInstances", map[string]string{
			"RegionId":   region,
			"PageSize":   "100",
			"PageNumber": strconv.Itoa(page),
		}, &resp)
		if err != nil {
			return nil, err
		}

		for _, item := range resp.Instances.Instance {
			instance := &CloudInstance{
				Id:         item.InstanceId,
				Name:       item.InstanceName,
				Region:     region,
				Type:       item.InstanceType,
				Status:     item.Status,
				Os:         getOsFromName(item.OSType + " " + item.OSName),
				PrivateIps: append(item.VpcAttributes.PrivateIpAddress.IpAddress, item.InnerIpAddress.IpAddress...),
				PublicIps:  item.PublicIpAddress.IpAddress,
			}
			if item.EipAddress.IpAddress != "" {
				instance.PublicIps = append([]string{item.EipAddress.IpAddress}, instance.PublicIps...)
			}
			res = append(res, instance)
		}
		if len(resp.Instances.Instance) == 0 || len(res) >= resp.TotalCount {
			return res, nil
		}
	}
}

// AWS EC2, with the query API and Signature Version 4.

func awsCall(account *CloudAccount, region string, params map[string]string, v interface{}) error {
	host := "ec2." + region + ".amazonaws.com"
	params["Version"] = "2016-11-15"
	query := getCanonicalQuery(params)

	amzDate := time.Now().UTC().Format("20060102T150405Z")
	req, err := http.NewRequest(http.MethodGet, "https://"+host+"/?"+query, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("Authorization", getAwsAuthorization(account.AccessKey, account.AccessSecret, host, region, query, amzDate))

	status, body, err := readCloudResponse(req)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		var apiErr struct {
			Errors []struct {
				Code    string `xml:"Code"`
				Message string `xml:"Message"`
			} `xml:"Errors>Error"`
		}
		if xml.Unmarshal(body, &apiErr) == nil && len(apiErr.Errors) > 0 {
			return fmt.Errorf("%s: %s", apiErr.Errors[0].Code, apiErr.Errors[0].Message)
		}
		return fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	return xml.Unmarshal(body, v)
}

// getAwsAuthorization signs a GET request of the query API with Signature Version 4.
func getAwsAuthorization(accessKey string, secret string, host string, region string, query string, amzDate string) string {
	date := amzDate[:8]
	scope := date + "/" + region + "/ec2/aws4_request"
	canonicalRequest := strings.Join([]string{
		http.MethodGet,
		"/",
		query,
		"host:" + host + "\nx-amz-date:" + amzDate + "\n",
		"host;x-amz-date",
		sha256Hex(nil),
	}, "\n")
	stringToSign := strings.Join([]string{"AWS4-HMAC-SHA256", amzDate, scope, sha256Hex([]byte(canonicalRequest))}, "\n")

	key := hmacSha256([]byte("AWS4"+secret), date)
	key = hmacSha256(key, region)
	key = hmacSha256(key, "ec2")
	key = hmacSha256(key, "aws4_request")
	signature := hex.EncodeToString(hmacSha256(key, stringToSign))
	return fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=host;x-amz-date, Signature=%s", accessKey, scope, signature)
}

func listAwsRegions(account *CloudAccount) ([]string, error) {
	var resp struct {
		Regions []string `xml:"regionInfo>item>regionName"`
	}
	err := awsCall(account, "us-east-1", map[string]string{"Action": "DescribeRegions"}, &resp)
	if err != nil {
		return nil, err
	}
	sort.Strings(resp.Regions)
	return resp.Regions, nil
}

func listAwsInstances(account *CloudAccount, region string) ([]*CloudInstance, error) {
	res := []*CloudInstance{}
	token := ""
	for {
		params := map[string]string{"Action": "DescribeInstances", "MaxResults": "1000"}
		if token != "" {
			params["NextToken"] = token
		}
		var resp struct {
			Instances []struct {
				InstanceId      string `xml:"instanceId"`
				InstanceType    string `xml:"instanceType"`
				State           string `xml:"instanceState>name"`
				PrivateIp       string `xml:"privateIpAddress"`
				PublicIp        string `xml:"ipAddress"`
				Platform        string `xml:"platform"`
				PlatformDetails string `xml:"platformDetails"`
				Tags            []struct {
					Key   string `xml:"key"`
					Value string `xml:"value"`
				} `xml:"tagSet>item"`
			} `xml:"reservationSet>item>instancesSet>item"`
			NextToken string `xml:"nextToken"`
		}
		err := awsCall(account, region, params, &resp)
		if err != nil {
			return nil, err
		}

		for _, item := range resp.Instances {
			if item.State == "terminated" {
				continue
			}
			instance := &CloudInstance{
				Id:     item.InstanceId,
				Region: region,
				Type:   item.InstanceType,
				Status: item.State,
				Os:     getOsFromName(item.Platform + " " + item.PlatformDetails),
			}
			for _, tag := range item.Tags {
				if tag.Key == "Name" {
					instance.Name = tag.Value
				}
			}
			if item.PrivateIp != "" {
				instance.PrivateIps = []string{item.PrivateIp}
			}
			if item.PublicIp != "" {
				instance.PublicIps = []string{item.PublicIp}
			}
			res = append(res, instance)
		}
		if resp.NextToken == "" {
			return res, nil
		}
		token = resp.NextToken
	}
}

// Tencent Cloud CVM, with the TC3-HMAC-SHA256 signature.

func getTencentAuthorization(secretId string, secretKey string, host string, contentType string, payload []byte, timestamp int64) string {
	date := time.Unix(timestamp, 0).UTC().Format("2006-01-02")
	service := strings.Split(host, ".")[0]
	canonicalHeaders := "content-type:" + contentType + "\nhost:" + host + "\n"
	signedHeaders := "content-type;host"
	canonicalRequest := strings.Join([]string{http.MethodPost, "/", "", canonicalHeaders, signedHeaders, sha256Hex(payload)}, "\n")
	scope := date + "/" + service + "/tc3_request"
	stringToSign := strings.Join([]string{"TC3-HMAC-SHA256", strconv.FormatInt(timestamp, 10), scope, sha256Hex([]byte(canonicalRequest))}, "\n")

	key := hmacSha256([]byte("TC3"+secretKey), date)
	key = hmacSha256(key, service)
	key = hmacSha256(key, "tc3_request")
	signature := hex.EncodeToString(hmacSha256(key, stringToSign))
	return fmt.Sprintf("TC3-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", secretId, scope, signedHeaders, signature)
}

func tencentCall(account *CloudAccount, region string, action string, params map[string]interface{}, v interface{}) error {
	host := "cvm.tencentcloudapi.com"
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}

	timestamp := time.Now().Unix()
	req, err := http.NewRequest(http.MethodPost, "https://"+host, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TC-Action", action)
	req.Header.Set("X-TC-Version", "2017-03-12")
	req.Header.Set("X-TC-Timestamp", strconv.FormatInt(timestamp, 10))
	if region != "" {
		req.Header.Set("X-TC-Region", region)
	}
	req.Header.Set("Authorization", getTencentAuthorization(account.AccessKey, account.AccessSecret, host, "application/json", payload, timestamp))

	status, body, err := readCloudResponse(req)
	if err != nil {
		return err
	}
	var apiErr struct {
		Response struct {
			Error *struct {
				Code    string
				Message string
			}
		}
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Response.Error != nil {
		return fmt.Errorf("%s: %s", apiErr.Response.Error.Code, apiErr.Response.Error.Message)
	}
	if status != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, v)
}

func listTencentRegions(account *CloudAccount) ([]string, error) {
	var resp struct {
		Response struct {
			RegionSet []struct {
				Region      string
				RegionState string
			}
		}
	}
	err := tencentCall(account, "", "DescribeRegions", map[string]interface{}{}, &resp)
	if err != nil {
		return nil, err
	}

	res := []string{}
	for _, region := range resp.Response.RegionSet {
		if region.RegionState == "" || region.RegionState == "AVAILABLE" {
			res = append(res, region.Region)
		}
	}
	sort.Strings(res)
	return res, nil
}

func listTencentInstances(account *CloudAccount, region string) ([]*CloudInstance, error) {
	res := []*CloudInstance{}
	for offset := 0; ; offset += 100 {
		var resp struct {
			Response struct {
				TotalCount  int
				InstanceSet []struct {
					InstanceId         string
					InstanceName       string
					InstanceType       string
					InstanceState      string
					OsName             string
					PrivateIpAddresses []string
					PublicIpAddresses  []string
				}
			}
		}
		err := tencentCall(account, region, "DescribeInstances", map[string]interface{}{"Offset": offset, "Limit": 100}, &resp)
		if err != nil {
			return nil, err
		}

		for _, item := range resp.Response.InstanceSet {
			res = append(res, &CloudInstance{
				Id:         item.InstanceId,
				Name:       item.InstanceName,
				Region:     region,
				Type:       item.InstanceType,
				Status:     item.InstanceState,
				Os:         getOsFromName(item.OsName),
				PrivateIps: item.PrivateIpAddresses,
				PublicIps:  item.PublicIpAddresses,
			})
		}
		if len(resp.Response.InstanceSet) == 0 || len(res) >= resp.Response.TotalCount {
			return res, nil
		}
	}
}

// A JSON API with a bearer token, for Azure and GCP.

func getJsonWithToken(rawUrl string, token string, v interface{}) error {
	req, err := http.NewRequest(http.MethodGet, rawUrl, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	status, body, err := readCloudResponse(req)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		var apiErr struct {
			Error json.RawMessage `json:"error"`
		}
		if json.Unmarshal(body, &apiErr) == nil && len(apiErr.Error) != 0 {
			var detail struct {
				Code    interface{} `json:"code"`
				Message string      `json:"message"`
			}
			if json.Unmarshal(apiErr.Error, &detail) == nil && detail.Message != "" {
				return fmt.Errorf("%v: %s", detail.Code, detail.Message)
			}
		}
		return fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, v)
}

func postTokenForm(tokenUrl string, form url.Values) (string, error) {
	req, err := http.NewRequest(http.MethodPost, tokenUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	status, body, err := readCloudResponse(req)
	if err != nil {
		return "", err
	}
	var resp struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.Unmarshal(body, &resp)
	if resp.AccessToken == "" {
		if resp.Error != "" {
			return "", fmt.Errorf("%s: %s", resp.Error, strings.SplitN(resp.ErrorDescription, "\r\n", 2)[0])
		}
		return "", fmt.Errorf("HTTP %d: %s", status, strings.TrimSpace(string(body)))
	}
	return resp.AccessToken, nil
}

// Azure virtual machines, with a service principal: tenant ID, client ID and client secret.

const azureManagementUrl = "https://management.azure.com"

func getAzureToken(account *CloudAccount) (string, error) {
	return postTokenForm("https://login.microsoftonline.com/"+url.PathEscape(account.TenantId)+"/oauth2/v2.0/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {account.AccessKey},
		"client_secret": {account.AccessSecret},
		"scope":         {azureManagementUrl + "/.default"},
	})
}

// listAzure follows the nextLink of a list of the subscription.
func listAzure(account *CloudAccount, token string, path string, apiVersion string, query string, each func(raw json.RawMessage) error) error {
	next := fmt.Sprintf("%s/subscriptions/%s/%s?api-version=%s%s", azureManagementUrl, url.PathEscape(account.SubscriptionId), path, apiVersion, query)
	for next != "" {
		var resp struct {
			Value    []json.RawMessage `json:"value"`
			NextLink string            `json:"nextLink"`
		}
		err := getJsonWithToken(next, token, &resp)
		if err != nil {
			return err
		}
		for _, raw := range resp.Value {
			err = each(raw)
			if err != nil {
				return err
			}
		}
		if resp.NextLink != "" && !strings.HasPrefix(resp.NextLink, azureManagementUrl+"/") {
			return fmt.Errorf("unexpected next link: %s", resp.NextLink)
		}
		next = resp.NextLink
	}
	return nil
}

func listAzureRegions(account *CloudAccount) ([]string, error) {
	token, err := getAzureToken(account)
	if err != nil {
		return nil, err
	}

	res := []string{}
	err = listAzure(account, token, "locations", "2022-12-01", "", func(raw json.RawMessage) error {
		var location struct {
			Name     string `json:"name"`
			Metadata struct {
				RegionType string `json:"regionType"`
			} `json:"metadata"`
		}
		err := json.Unmarshal(raw, &location)
		if err == nil && location.Metadata.RegionType != "Logical" {
			res = append(res, location.Name)
		}
		return err
	})
	sort.Strings(res)
	return res, err
}

func listAzureInstances(account *CloudAccount) ([]*CloudInstance, error) {
	token, err := getAzureToken(account)
	if err != nil {
		return nil, err
	}

	publicIps := map[string]string{}
	err = listAzure(account, token, "providers/Microsoft.Network/publicIPAddresses", "2024-05-01", "", func(raw json.RawMessage) error {
		var ip struct {
			Id         string `json:"id"`
			Properties struct {
				IpAddress string `json:"ipAddress"`
			} `json:"properties"`
		}
		err := json.Unmarshal(raw, &ip)
		publicIps[strings.ToLower(ip.Id)] = ip.Properties.IpAddress
		return err
	})
	if err != nil {
		return nil, err
	}

	type nicIps struct{ private, public []string }
	nics := map[string]*nicIps{}
	err = listAzure(account, token, "providers/Microsoft.Network/networkInterfaces", "2024-05-01", "", func(raw json.RawMessage) error {
		var nic struct {
			Id         string `json:"id"`
			Properties struct {
				IpConfigurations []struct {
					Properties struct {
						PrivateIpAddress string `json:"privateIPAddress"`
						PublicIpAddress  *struct {
							Id string `json:"id"`
						} `json:"publicIPAddress"`
					} `json:"properties"`
				} `json:"ipConfigurations"`
			} `json:"properties"`
		}
		err := json.Unmarshal(raw, &nic)
		ips := &nicIps{}
		for _, config := range nic.Properties.IpConfigurations {
			if config.Properties.PrivateIpAddress != "" {
				ips.private = append(ips.private, config.Properties.PrivateIpAddress)
			}
			if config.Properties.PublicIpAddress != nil {
				if ip := publicIps[strings.ToLower(config.Properties.PublicIpAddress.Id)]; ip != "" {
					ips.public = append(ips.public, ip)
				}
			}
		}
		nics[strings.ToLower(nic.Id)] = ips
		return err
	})
	if err != nil {
		return nil, err
	}

	powerStates := map[string]string{}
	err = listAzure(account, token, "providers/Microsoft.Compute/virtualMachines", "2024-07-01", "&statusOnly=true", func(raw json.RawMessage) error {
		var vm struct {
			Id         string `json:"id"`
			Properties struct {
				InstanceView struct {
					Statuses []struct {
						Code string `json:"code"`
					} `json:"statuses"`
				} `json:"instanceView"`
			} `json:"properties"`
		}
		err := json.Unmarshal(raw, &vm)
		for _, status := range vm.Properties.InstanceView.Statuses {
			if strings.HasPrefix(status.Code, "PowerState/") {
				powerStates[strings.ToLower(vm.Id)] = strings.TrimPrefix(status.Code, "PowerState/")
			}
		}
		return err
	})
	if err != nil {
		return nil, err
	}

	res := []*CloudInstance{}
	err = listAzure(account, token, "providers/Microsoft.Compute/virtualMachines", "2024-07-01", "", func(raw json.RawMessage) error {
		var vm struct {
			Id         string `json:"id"`
			Name       string `json:"name"`
			Location   string `json:"location"`
			Properties struct {
				VmId            string `json:"vmId"`
				HardwareProfile struct {
					VmSize string `json:"vmSize"`
				} `json:"hardwareProfile"`
				StorageProfile struct {
					OsDisk struct {
						OsType string `json:"osType"`
					} `json:"osDisk"`
				} `json:"storageProfile"`
				NetworkProfile struct {
					NetworkInterfaces []struct {
						Id string `json:"id"`
					} `json:"networkInterfaces"`
				} `json:"networkProfile"`
			} `json:"properties"`
		}
		err := json.Unmarshal(raw, &vm)
		if err != nil {
			return err
		}

		instance := &CloudInstance{
			Id:     vm.Properties.VmId,
			Name:   vm.Name,
			Region: vm.Location,
			Type:   vm.Properties.HardwareProfile.VmSize,
			Status: powerStates[strings.ToLower(vm.Id)],
			Os:     getOsFromName(vm.Properties.StorageProfile.OsDisk.OsType),
		}
		if instance.Id == "" {
			instance.Id = vm.Id
		}
		for _, ref := range vm.Properties.NetworkProfile.NetworkInterfaces {
			if ips := nics[strings.ToLower(ref.Id)]; ips != nil {
				instance.PrivateIps = append(instance.PrivateIps, ips.private...)
				instance.PublicIps = append(instance.PublicIps, ips.public...)
			}
		}
		res = append(res, instance)
		return nil
	})
	return res, err
}

// Google Compute Engine, with the JSON key of a service account.

type gcpServiceAccountKey struct {
	ProjectId    string `json:"project_id"`
	PrivateKeyId string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	TokenUri     string `json:"token_uri"`
}

func getGcpKey(account *CloudAccount) (*gcpServiceAccountKey, error) {
	key := &gcpServiceAccountKey{}
	err := json.Unmarshal([]byte(account.AccessSecret), key)
	if err != nil || key.ClientEmail == "" || key.PrivateKey == "" {
		return nil, fmt.Errorf("the secret should be the JSON key of a service account")
	}
	return key, nil
}

func getGcpProject(account *CloudAccount) (string, error) {
	if account.ProjectId != "" {
		return account.ProjectId, nil
	}
	key, err := getGcpKey(account)
	if err != nil {
		return "", err
	}
	return key.ProjectId, nil
}

func getGcpToken(account *CloudAccount) (string, error) {
	key, err := getGcpKey(account)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode([]byte(key.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("the private key of the service account can't be read")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", err
	}
	privateKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("the private key of the service account is not an RSA key")
	}

	// the token endpoint is Google's, whatever the key says, so that the signed assertion goes nowhere else
	tokenUrl := "https://oauth2.googleapis.com/token"
	now := time.Now().Unix()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": key.PrivateKeyId})
	claims, _ := json.Marshal(map[string]interface{}{
		"iss":   key.ClientEmail,
		"scope": "https://www.googleapis.com/auth/compute.readonly",
		"aud":   tokenUrl,
		"iat":   now,
		"exp":   now + 3600,
	})
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}

	return postTokenForm(tokenUrl, url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)},
	})
}

func getLastPathPart(s string) string {
	return s[strings.LastIndex(s, "/")+1:]
}

func listGcpRegions(account *CloudAccount) ([]string, error) {
	project, err := getGcpProject(account)
	if err != nil {
		return nil, err
	}
	token, err := getGcpToken(account)
	if err != nil {
		return nil, err
	}

	res := []string{}
	pageToken := ""
	for {
		var resp struct {
			Items []struct {
				Name string `json:"name"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		err = getJsonWithToken(fmt.Sprintf("https://compute.googleapis.com/compute/v1/projects/%s/regions?pageToken=%s", url.PathEscape(project), url.QueryEscape(pageToken)), token, &resp)
		if err != nil {
			return nil, err
		}
		for _, item := range resp.Items {
			res = append(res, item.Name)
		}
		if resp.NextPageToken == "" {
			sort.Strings(res)
			return res, nil
		}
		pageToken = resp.NextPageToken
	}
}

func listGcpInstances(account *CloudAccount) ([]*CloudInstance, error) {
	project, err := getGcpProject(account)
	if err != nil {
		return nil, err
	}
	token, err := getGcpToken(account)
	if err != nil {
		return nil, err
	}

	res := []*CloudInstance{}
	pageToken := ""
	for {
		var resp struct {
			Items map[string]struct {
				Instances []struct {
					Id                string `json:"id"`
					Name              string `json:"name"`
					Zone              string `json:"zone"`
					MachineType       string `json:"machineType"`
					Status            string `json:"status"`
					NetworkInterfaces []struct {
						NetworkIp     string `json:"networkIP"`
						AccessConfigs []struct {
							NatIp string `json:"natIP"`
						} `json:"accessConfigs"`
					} `json:"networkInterfaces"`
					Disks []struct {
						Licenses        []string `json:"licenses"`
						GuestOsFeatures []struct {
							Type string `json:"type"`
						} `json:"guestOsFeatures"`
					} `json:"disks"`
				} `json:"instances"`
			} `json:"items"`
			NextPageToken string `json:"nextPageToken"`
		}
		err = getJsonWithToken(fmt.Sprintf("https://compute.googleapis.com/compute/v1/projects/%s/aggregated/instances?maxResults=500&pageToken=%s", url.PathEscape(project), url.QueryEscape(pageToken)), token, &resp)
		if err != nil {
			return nil, err
		}

		for _, scope := range resp.Items {
			for _, item := range scope.Instances {
				zone := getLastPathPart(item.Zone)
				region := zone
				if i := strings.LastIndex(zone, "-"); i > 0 {
					region = zone[:i]
				}
				os := "Linux"
				for _, disk := range item.Disks {
					for _, license := range disk.Licenses {
						if strings.Contains(getLastPathPart(license), "windows") {
							os = "Windows"
						}
					}
					for _, feature := range disk.GuestOsFeatures {
						if feature.Type == "WINDOWS" {
							os = "Windows"
						}
					}
				}

				instance := &CloudInstance{
					Id:     item.Id,
					Name:   item.Name,
					Region: region,
					Type:   getLastPathPart(item.MachineType),
					Status: item.Status,
					Os:     os,
				}
				for _, nic := range item.NetworkInterfaces {
					if nic.NetworkIp != "" {
						instance.PrivateIps = append(instance.PrivateIps, nic.NetworkIp)
					}
					for _, config := range nic.AccessConfigs {
						if config.NatIp != "" {
							instance.PublicIps = append(instance.PublicIps, config.NatIp)
						}
					}
				}
				res = append(res, instance)
			}
		}
		if resp.NextPageToken == "" {
			return res, nil
		}
		pageToken = resp.NextPageToken
	}
}
