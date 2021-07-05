package dashboard_reporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
)

type Reporter struct {
	APIKey    string
	APISecret string
}
type ScanLog struct {
	Log_type             string `json:"log_type"`
	Text                 string `json:"text"`
	Additional_text_type string `json:"additional_text_type"`
	Additional_text      string `json:"additional_text"`
}

type ScanLogEntries struct {
	Scan_log_entries []ScanLog `json:"scan_log_entries"`
}


func (r *Reporter) AddScanLogs(id int, scanLogs []ScanLog) error {
	pluginUpdateURL := fmt.Sprintf("%s/api/v1/user_plugins/%d/add_scan_logs.json", os.Getenv("DASHBOARD_API_URL"), id)

	json_array := ScanLogEntries{
		Scan_log_entries: scanLogs,
	}

	body, err := json.Marshal(json_array)
	if err != nil {
		return fmt.Errorf("Failed to marshal body: %s", err.Error())
	}

	client := &http.Client{}
	req, err := http.NewRequest("POST", pluginUpdateURL, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("Failed to initialize HTTP request: %s", err.Error())
	}

	req.Header.Set("Authorization", fmt.Sprintf("Token %s:%s", r.APIKey, r.APISecret))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("Failed to perform API request to dashboard: %s", err.Error())
	}

	if resp.StatusCode != 200 {
		respBody, err := ioutil.ReadAll(resp.Body)
		errorMessage := fmt.Sprintf("Failed to update plugin status, status code %d.", resp.StatusCode)
		if err != nil {
			return fmt.Errorf(errorMessage)
		}

		return fmt.Errorf(fmt.Sprintf("%s Response body: %s", errorMessage, string(respBody)))
	}
	return nil
}


