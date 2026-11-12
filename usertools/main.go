package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Println("Usage: go run main.go <jsonfile> <url>")
		os.Exit(1)
	}

	jsonFile := os.Args[1]
	url := os.Args[2]

	data, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Printf("Failed to read JSON file: %v\n", err)
		os.Exit(1)
	}

	var jsonData map[string]interface{}
	if err := json.Unmarshal(data, &jsonData); err != nil {
		fmt.Printf("Invalid JSON format: %v\n", err)
		os.Exit(1)
	}

	resp, err := http.Post(url, "application/json", bytes.NewBuffer(data))
	if err != nil {
		fmt.Printf("Failed to send request: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("Failed to read response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Response:")
	fmt.Println(string(respBody))
}
