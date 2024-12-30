package main

import (
	"bufio"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func postRequest(url string, data string) {
	log.Printf("posting task: %s?%s ...", url, data)
	req, err := http.NewRequest("POST", url, strings.NewReader(data))
	if err != nil {
		log.Printf("error creating request: %v", err)
		return
	}
	req.Header.Add("X-Appengine-Taskname", "local")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("error creating request: %v", err)
		return
	} else {
		log.Printf("posted task: %s?%s, status: %s", url, data, resp.Status)
	}
}

func processFile(file *os.File, offset int64) (int64, error) {
	file.Seek(offset, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		record := strings.Split(scanner.Text(), " ")
		postRequest(record[1], record[0])
	}
	return file.Seek(0, 1)
}

func main() {
	file, err := os.Open(os.Args[1])
	if err != nil {
		log.Fatalf("error opening file: %s, %v", os.Args[1], err)
	}
	defer file.Close()
	offset := int64(0)
	for {
		offset, err = processFile(file, offset)
		if err != nil {
			log.Fatalf("error processing file: %v", err)
		}
		time.Sleep(time.Second)
	}
}
