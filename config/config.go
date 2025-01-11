package config

import (
	"context"
	"encoding/json"
	"io"
	"os"

	"github.com/harishjp/goread/log"
)

var config Config

type Config struct {
	AdminEmail  string `json:"adminEmail"`
	ClientID    string `json:"clientID"`
	RootURL     string `json:"rootURL"`
	Region      string `json:"region"`
	IsDevServer bool
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
