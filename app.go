package main

import (
	"net/http"
	"os"

	"github.com/andyfusniak/stackdriver-gae-logrus-plugin"
	_ "github.com/harishjp/goread/appstats"
	_ "github.com/harishjp/goread/goapp"
	log "github.com/sirupsen/logrus"
)

func registerLog() {
	formatter := stackdriver.GAEStandardFormatter()
	if projectId := os.Getenv("GOOGLE_CLOUD_PROJECT"); projectId != "" {
		formatter = stackdriver.GAEStandardFormatter(stackdriver.WithProjectID(projectId))
	}
	log.SetFormatter(formatter)
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
		log.Printf("Defaulting to port %s", port)
	}

	log.Printf("Listening on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}
