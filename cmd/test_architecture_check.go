package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/test_architecture_check.go <project-path>")
		os.Exit(1)
	}

	projectPath := os.Args[1]
	
	if err := runArchitectureCheck(projectPath); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
