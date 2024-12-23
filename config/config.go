package config

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/harishjp/goread/log"
)

var config Config

type Config struct {
	AdminEmail  string `json:"adminEmail"`
	ClientID    string `json:"clientID"`
	RootURL     string `json:"rootURL"`
	IsDevServer bool
	Region      string
	ProjectID   string
}

func LoadConfig() {
	jsonFile, err := os.Open("config.json")
	if err != nil {
		panic(err)
	}
	defer jsonFile.Close()
	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(byteValue, &config)
	if err != nil {
		panic(err)
	}

	config.IsDevServer = os.Getenv("GAE_DEPLOYMENT_ID") == ""
	if config.IsDevServer {
		config.Region = "local"
		config.ProjectID = "local"
	} else {
		config.Region = getMetaData("instance/region")
		config.ProjectID = os.Getenv("GOOGLE_CLOUD_PROJECT")
	}

	bytes, _ := json.Marshal(config)
	log.Infof(context.Background(), "Loaded config: %v", string(bytes))
}

func IsDevServer() bool {
	return config.IsDevServer
}

func ProjectID() string {
	return config.ProjectID
}

func Region() string {
	return config.Region
}

func AdminEmail() string {
	return config.AdminEmail
}

func ClientID() string {
	return config.ClientID
}

func RootURL() string {
	if IsDevServer() {
		return "http://localhost:8080"
	} else {
		return config.RootURL
	}
}

func getMetaData(suffix string) string {
	req, err := http.NewRequest("GET", "https://metadata/computeMetadata/v1/"+suffix, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Add("Metadata-Flavor", "Google")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	return string(body)
}
