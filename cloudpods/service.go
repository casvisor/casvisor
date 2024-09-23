package cloudpods

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/beego/beego"
)

var (
	apiInstance *Api
	accessKey   string // Fetched from app configuration
	secretKey   string // Fetched from app configuration
)

type Api struct {
	BaseURL string
}

// CloudResource request and response structure
type CloudResource struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	ResourceType string `json:"resource_type"`
}

func init() {
	initCloudPodsApi()
}

// Initialize CloudPods API and set the service's BaseURL
func initCloudPodsApi() {
	apiInstance = &Api{
		BaseURL: beego.AppConfig.String("cloudpodsEndpoint") + "/api/s/identity/v3", // Replace with actual endpoint
	}
	accessKey = beego.AppConfig.String("cloudpodsAccessKey")
	secretKey = beego.AppConfig.String("cloudpodsSecretKey")
}

// Generate signature
func signString(secret, stringToSign string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	return hex.EncodeToString(h.Sum(nil))
}

// General API call function
func callAPI(method, url string, body []byte) ([]byte, error) {
	client := &http.Client{}
	req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}

	// Add authentication information
	timestamp := time.Now().UTC().Format(time.RFC3339)
	stringToSign := method + "\n" + url + "\n" + timestamp
	signature := signString(secretKey, stringToSign)

	req.Header.Set("X-Auth-Timestamp", timestamp)
	req.Header.Set("X-Auth-Key", accessKey)
	req.Header.Set("X-Auth-Signature", signature)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API call failed with status code %d: %s", resp.StatusCode, string(responseBody))
	}

	return responseBody, nil
}

// Create, Update, and Delete Cloud Resource
func CreateResource(resource CloudResource) (*CloudResource, error) {
	return modifyResource(http.MethodPost, resource)
}

func UpdateResource(resource CloudResource) (*CloudResource, error) {
	resource.ID = fmt.Sprintf("%s", resource.ID) // Ensure ID is set
	return modifyResource(http.MethodPut, resource)
}

func modifyResource(method string, resource CloudResource) (*CloudResource, error) {
	url := fmt.Sprintf("%s/v1/resources%s%s", apiInstance.BaseURL, func() string {
		if method == http.MethodPut {
			return "/" + resource.ID
		}
		return ""
	}(), "")

	jsonData, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}

	responseBody, err := callAPI(method, url, jsonData)
	if err != nil {
		return nil, err
	}

	var modifiedResource CloudResource
	if err := json.Unmarshal(responseBody, &modifiedResource); err != nil {
		return nil, err
	}

	return &modifiedResource, nil
}

func DeleteResource(resourceID string) error {
	url := fmt.Sprintf("%s/v1/resources/%s", apiInstance.BaseURL, resourceID)
	_, err := callAPI(http.MethodDelete, url, nil)
	return err
}

// Get Cloud Resource
func GetResource(resourceID string) (*CloudResource, error) {
	url := fmt.Sprintf("%s/v1/resources/%s", apiInstance.BaseURL, resourceID)
	responseBody, err := callAPI(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	var resource CloudResource
	if err := json.Unmarshal(responseBody, &resource); err != nil {
		return nil, err
	}

	return &resource, nil
}

// Restart Cloud Resource
func RestartResource(resourceID string) error {
	url := fmt.Sprintf("%s/v1/resources/%s/restart", apiInstance.BaseURL, resourceID)
	_, err := callAPI(http.MethodPost, url, nil)
	return err
}
