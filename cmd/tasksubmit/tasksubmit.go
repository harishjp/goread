package main

import (
	"bufio"
	"flag"
	"io"
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
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return -1, err
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		record := strings.Split(scanner.Text(), " ")
		postRequest(record[1], record[0])
	}
	return file.Seek(0, io.SeekEnd)
}

func main() {
	tail := flag.Bool("t", false, "read from end of file")
	flag.Parse()

	fileName := flag.Arg(0)
	if fileName == "" {
		fileName = "task.txt"
	}

	// open file create if it does not exist
	file, err := os.OpenFile(fileName, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		log.Fatalf("error opening file: %s, %v", fileName, err)
	}
	defer func() { _ = file.Close() }()

	// compute offset, if -t specified then scan from end of file.
	offset := int64(0)
	if *tail {
		offset, err = file.Seek(0, io.SeekEnd)
		if err != nil {
			log.Fatalf("error seeking to end of file: %s, %v", fileName, err)
		}
	}
	log.Printf("reading file %s from offset: %d", fileName, offset)

	for {
		offset, err = processFile(file, offset)
		if err != nil {
			log.Fatalf("error processing file: %v", err)
		}
		time.Sleep(time.Second)
	}
}
