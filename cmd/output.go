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
	Score         int    `json:"score"`
	Likelihood    string `json:"likelihood"`
	ScoreReason   string `json:"score_reason,omitempty"`
	BodyHash      string `json:"body_hash,omitempty"`
	Location      string `json:"location,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	Server        string `json:"server,omitempty"`
	ReproCurl     string `json:"repro_curl,omitempty"`
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

	if jsonOutput || jsonLines {
		jsonResultsMutex.Lock()
		var data []byte
		var err error
		if jsonLines {
			var lines []byte
			for _, item := range jsonResults {
				line, marshalErr := json.Marshal(item)
				if marshalErr != nil {
					err = marshalErr
					break
				}
				lines = append(lines, line...)
				lines = append(lines, '\n')
			}
			data = lines
		} else {
			data, err = json.MarshalIndent(jsonResults, "", "  ")
			if err == nil {
				data = append(data, '\n')
			}
		}
		jsonResultsMutex.Unlock()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] Error marshaling JSON: %v\n", err)
		} else {
			outputWriter.Write(data)
		}
	}

	outputWriter.Close()
	outputWriter = nil
}

// writeResultToOutput writes a result to the output file.
// In JSON mode, it accumulates results (thread-safe via jsonResultsMutex).
// In plain mode, it writes immediately — caller MUST hold printMutex.
func writeResultToOutput(result Result, technique string) {
	if outputWriter == nil && !jsonOutput && !jsonLines {
		return
	}

	if jsonOutput || jsonLines {
		jsonResultsMutex.Lock()
		jsonResults = append(jsonResults, JSONResult{
			StatusCode:    result.statusCode,
			ContentLength: result.contentLength,
			Technique:     technique,
			Payload:       result.line,
			Score:         result.score,
			Likelihood:    result.likelihood,
			ScoreReason:   result.scoreReason,
			BodyHash:      result.bodyHash,
			Location:      result.location,
			ContentType:   result.contentType,
			Server:        result.server,
			ReproCurl:     result.reproCurl,
		})
		jsonResultsMutex.Unlock()

		// If no output file, write JSON to stdout at close
		return
	}

	if outputWriter != nil {
		fmt.Fprintf(outputWriter, "%d\t[%d %s]\t%d bytes\t%s\n", result.statusCode, result.score, result.likelihood, result.contentLength, result.line)
		if result.reproCurl != "" {
			fmt.Fprintf(outputWriter, "curl\t%s\n", result.reproCurl)
		}
	}
}

// flushJSONToStdout writes JSON results to stdout when no output file is specified.
func flushJSONToStdout() {
	if (!jsonOutput && !jsonLines) || outputWriter != nil {
		return
	}

	jsonResultsMutex.Lock()
	defer jsonResultsMutex.Unlock()

	if len(jsonResults) == 0 {
		return
	}

	if jsonLines {
		for _, item := range jsonResults {
			data, err := json.Marshal(item)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[!] Error marshaling JSON: %v\n", err)
				return
			}
			fmt.Println(string(data))
		}
		return
	}
	data, err := json.MarshalIndent(jsonResults, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Error marshaling JSON: %v\n", err)
		return
	}
	fmt.Println(string(data))
}
