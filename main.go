package main

import (
	"flag"
	"fmt"
	"github.com/NoL1m1ts/go-oapi-merge/merge"
	"log"
)

func main() {
	inputFile := flag.String("input", "api.yaml", "Path to the main OpenAPI file (api.yaml)")
	outputFile := flag.String("output", "merged_api.yaml", "Path to the output file")
	flag.Parse()

	if err := merge.OapiYaml(*inputFile, *outputFile); err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Printf("File successfully merged and saved as %s\n", *outputFile)
}
