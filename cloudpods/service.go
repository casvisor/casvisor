package cloudpods

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/beego/beego"
)

var apiInstance *Api

type Api struct {
	BaseURL string
}

// CloudResource requestBody and responseBody
type CloudResource struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	ResourceType string `json:"resource_type"`
}

func init() {
	initCloudPodsApi()
}

func initCloudPodsApi() {
	BaseURL := beego.AppConfig.String("cloudpodsEndpoint")
	apiInstance = &Api{
		BaseURL: BaseURL,
	}
}

func CreateResource(resource CloudResource) (*CloudResource, error) {
	url := fmt.Sprintf("%s/v1/resources", apiInstance.BaseURL)
	jsonData, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var createdResource CloudResource
	if err := json.NewDecoder(resp.Body).Decode(&createdResource); err != nil {
		return nil, err
	}

	return &createdResource, nil
}

func DeleteResource(resourceID string) error {
	url := fmt.Sprintf("%s/v1/resources/%s", apiInstance.BaseURL, resourceID)
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete resource, status code: %d", resp.StatusCode)
	}

	return nil
}

func UpdateResource(resource CloudResource) (*CloudResource, error) {
	url := fmt.Sprintf("%s/v1/resources/%s", apiInstance.BaseURL, resource.ID)
	jsonData, err := json.Marshal(resource)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var updatedResource CloudResource
	if err := json.NewDecoder(resp.Body).Decode(&updatedResource); err != nil {
		return nil, err
	}

	return &updatedResource, nil
}

func GetResource(resourceID string) (*CloudResource, error) {
	url := fmt.Sprintf("%s/v1/resources/%s", apiInstance.BaseURL, resourceID)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var resource CloudResource
	if err := json.NewDecoder(resp.Body).Decode(&resource); err != nil {
		return nil, err
	}

	return &resource, nil
}

func RestartResource(resourceID string) error {
	url := fmt.Sprintf("%s/v1/resources/%s/restart", apiInstance.BaseURL, resourceID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to restart resource, status code: %d", resp.StatusCode)
	}

	return nil
}
