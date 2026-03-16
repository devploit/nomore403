package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// JSONResult represents a single result in JSON output format.
type JSONResult struct {
	StatusCode    int    `json:"status_code"`
	ContentLength int    `json:"content_length"`
	Technique     string `json:"technique"`
	Payload       string `json:"payload"`
}

var (
	outputWriter     *os.File
	jsonResults      []JSONResult
	jsonResultsMutex sync.Mutex
)

// initOutputWriter opens the output file for writing.
func initOutputWriter(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	outputWriter = f
	return nil
}

// closeOutputWriter flushes and closes the output file.
// If JSON mode is enabled, writes the accumulated JSON results.
func closeOutputWriter() {
	if outputWriter == nil {
		return
	}

	if jsonOutput {
		jsonResultsMutex.Lock()
		data, err := json.MarshalIndent(jsonResults, "", "  ")
		jsonResultsMutex.Unlock()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] Error marshaling JSON: %v\n", err)
		} else {
			outputWriter.Write(data)
			outputWriter.Write([]byte("\n"))
		}
	}

	outputWriter.Close()
	outputWriter = nil
}

// writeResultToOutput writes a result to the output file.
// In JSON mode, it accumulates results (thread-safe via jsonResultsMutex).
// In plain mode, it writes immediately — caller MUST hold printMutex.
func writeResultToOutput(result Result, technique string) {
	if outputWriter == nil && !jsonOutput {
		return
	}

	if jsonOutput {
		jsonResultsMutex.Lock()
		jsonResults = append(jsonResults, JSONResult{
			StatusCode:    result.statusCode,
			ContentLength: result.contentLength,
			Technique:     technique,
			Payload:       result.line,
		})
		jsonResultsMutex.Unlock()

		// If no output file, write JSON to stdout at close
		return
	}

	if outputWriter != nil {
		fmt.Fprintf(outputWriter, "%d\t%d bytes\t%s\n", result.statusCode, result.contentLength, result.line)
	}
}

// flushJSONToStdout writes JSON results to stdout when no output file is specified.
func flushJSONToStdout() {
	if !jsonOutput || outputWriter != nil {
		return
	}

	jsonResultsMutex.Lock()
	defer jsonResultsMutex.Unlock()

	if len(jsonResults) == 0 {
		return
	}

	data, err := json.MarshalIndent(jsonResults, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Error marshaling JSON: %v\n", err)
		return
	}
	fmt.Println(string(data))
}
