package cmd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	bypassIP          string
	cfgFile           string
	delay             int
	folder            string
	httpMethod        string
	maxGoroutines     int
	nobanner          bool
	proxy             string
	randomAgent       bool
	rateLimit         bool
	timeout           int
	redirect          bool
	reqHeaders        []string
	requestFile       string
	schema            bool
	technique         []string
	uri               string
	userAgent         string
	verbose           bool
	statusCodes       []string
	uniqueOutput      bool
	outputFile        string
	jsonOutput        bool
	jsonLines         bool
	payloadPosition   string
	noCalibrate       bool
	strictCalibrate   bool
	retryCount        int
	retryBackoffMs    int
	hostDelayMs       int
	rawHTTP           bool
	topScoreMin       int
	variationScoreMin int
	topLimit          int
	techniqueExplicit bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "nomore403",
	Short: "Tool to bypass 40X response codes.",
	Long:  `Command line application that automates different ways to bypass 40X codes.`,

	Run: func(cmd *cobra.Command, args []string) {
		techniqueExplicit = cmd.Flags().Changed("technique")
		if len(folder) == 0 {
			folder = "payloads"
		}

		// Initialize output writer if -o flag is set
		if outputFile != "" {
			if err := initOutputWriter(outputFile); err != nil {
				log.Printf("[!] Error opening output file: %v", err)
				return
			}
			defer closeOutputWriter()
		}

		fi, err := os.Stdin.Stat()
		if err != nil {
			log.Printf("[!] Error reading stdin: %v", err)
			return
		}
		// Ensure JSON output is flushed at the end if writing to stdout
		defer flushJSONToStdout()

		if (fi.Mode() & os.ModeCharDevice) == 0 {
			bytes, err := io.ReadAll(os.Stdin)
			if err != nil {
				log.Printf("[!] Error reading stdin: %v", err)
				return
			}
			lines := strings.Split(string(bytes), "\n")
			var urls []string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				urls = append(urls, line)
			}
			runTargets(urls)
		} else {
			if len(requestFile) > 0 {
				loadFlagsFromRequestFile(requestFile, schema, verbose, technique, redirect)
			} else {
				if len(uri) == 0 {
					_ = cmd.Help()
					return
				}
				// Check if -u value is a file containing URLs
				urls := readURLsFromInput(uri)
				runTargets(urls)
			}
		}
	},
}

// SetVersionInfo sets the version information for the root command.
func SetVersionInfo(version, buildDate string) {
	rootCmd.Version = version
	rootCmd.SetVersionTemplate(fmt.Sprintf("nomore403 version %s (built %s)\n", version, buildDate))
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVarP(&bypassIP, "bypass-ip", "i", "", "Use a specified IP address or hostname for bypassing access controls. Injects this IP in headers like 'X-Forwarded-For'.")
	rootCmd.PersistentFlags().IntVarP(&delay, "delay", "d", 0, "Specify a delay between requests in milliseconds. Helps manage request rate (default: 0ms).")
	rootCmd.PersistentFlags().StringVarP(&folder, "folder", "f", "", "Specify the folder location for payloads if not in the same directory as the executable.")
	rootCmd.PersistentFlags().StringSliceVarP(&reqHeaders, "header", "H", []string{""}, "Add one or more custom headers to requests. Repeatable flag for multiple headers.")
	rootCmd.PersistentFlags().BoolVarP(&schema, "http", "", false, "Use HTTP instead of HTTPS for requests defined in the request file.")
	rootCmd.PersistentFlags().StringVarP(&httpMethod, "http-method", "t", "", "Specify the HTTP method for the request (e.g., GET, POST). Default is 'GET'.")
	rootCmd.PersistentFlags().IntVarP(&maxGoroutines, "max-goroutines", "m", 50, "Limit the maximum number of concurrent goroutines to manage load (default: 50).")
	rootCmd.PersistentFlags().BoolVarP(&nobanner, "no-banner", "", false, "Disable the display of the startup banner (default: banner shown).")
	rootCmd.PersistentFlags().StringVarP(&proxy, "proxy", "x", "", "Specify a proxy server for requests (e.g., 'http://server:port').")
	rootCmd.PersistentFlags().BoolVarP(&randomAgent, "random-agent", "", false, "Enable the use of a randomly selected User-Agent.")
	rootCmd.PersistentFlags().BoolVarP(&rateLimit, "rate-limit", "l", false, "Halt requests upon encountering a 429 (rate limit) HTTP status code.")
	rootCmd.PersistentFlags().BoolVarP(&redirect, "redirect", "r", false, "Automatically follow redirects in responses.")
	rootCmd.PersistentFlags().StringVarP(&requestFile, "request-file", "", "", "Load request configuration and flags from a specified file.")
	rootCmd.PersistentFlags().BoolVarP(&noCalibrate, "no-calibrate", "", false, "Disable auto-calibration filtering and always compare results against the default request baseline.")
	rootCmd.PersistentFlags().BoolVarP(&strictCalibrate, "strict-calibrate", "", false, "Use a stricter default-response comparison that also considers body hash and key response headers.")
	rootCmd.PersistentFlags().StringSliceVarP(&statusCodes, "status", "", []string{}, "Filter output by comma-separated status codes (e.g., 200,301,403)")
	rootCmd.PersistentFlags().StringSliceVarP(&technique, "technique", "k", []string{"verbs", "verbs-case", "headers", "endpaths", "midpaths", "double-encoding", "unicode", "http-versions", "http-parser", "path-case", "hop-by-hop", "absolute-uri", "method-override", "path-normalization", "suffix-tricks", "header-confusion", "host-override", "forwarded-trust", "proto-confusion", "ip-encoding", "raw-duplicates", "raw-authority", "raw-desync"}, "Specify one or more attack techniques to use (e.g., headers,path-case,unicode).")
	rootCmd.PersistentFlags().IntVarP(&timeout, "timeout", "", 6000, "Specify a max timeout time in ms.")
	rootCmd.PersistentFlags().IntVarP(&retryCount, "retry-count", "", 2, "Number of retries for transient errors and rate limiting when retrying is allowed.")
	rootCmd.PersistentFlags().IntVarP(&retryBackoffMs, "retry-backoff-ms", "", 500, "Base backoff in milliseconds used between retries.")
	rootCmd.PersistentFlags().IntVarP(&hostDelayMs, "host-delay", "", 0, "Minimum delay in milliseconds between batched targets on the same host.")
	rootCmd.PersistentFlags().IntVarP(&topLimit, "top", "", 10, "Maximum number of entries to show in each summary section (0 disables top summaries).")
	rootCmd.PersistentFlags().IntVarP(&topScoreMin, "top-score-min", "", 55, "Minimum score required for a result to appear in the Top Findings summary.")
	rootCmd.PersistentFlags().IntVarP(&variationScoreMin, "variation-score-min", "", 25, "Minimum score required for a result to appear in the Interesting Variations summary.")
	rootCmd.PersistentFlags().BoolVarP(&uniqueOutput, "unique", "", false, "Show unique output based on status code and response length.")
	rootCmd.PersistentFlags().StringVarP(&uri, "uri", "u", "", "Specify the target URL or a file containing URLs (one per line).")
	rootCmd.PersistentFlags().StringVarP(&userAgent, "user-agent", "a", "", "Specify a custom User-Agent string for requests (default: 'nomore403').")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for detailed request/response logging (not based on auto-calibrate).")
	rootCmd.PersistentFlags().StringVarP(&outputFile, "output", "o", "", "Save results to the specified file.")
	rootCmd.PersistentFlags().BoolVarP(&jsonOutput, "json", "", false, "Output results in JSON format.")
	rootCmd.PersistentFlags().BoolVarP(&jsonLines, "jsonl", "", false, "Output results as JSON Lines (one result per line).")
	rootCmd.PersistentFlags().BoolVarP(&rawHTTP, "raw-http", "", false, "Enable raw HTTP techniques that avoid net/http request normalization.")
	rootCmd.PersistentFlags().StringVarP(&payloadPosition, "payload-position", "p", "", "Marker in URL indicating where to insert payloads (e.g., §). Use in URL like: -u 'http://example.com/§100§/admin'.")
}

func runTargets(urls []string) {
	lastHostRun := make(map[string]time.Time)

	for _, target := range urls {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}

		if hostDelayMs > 0 {
			if parsed, err := url.Parse(target); err == nil && parsed.Host != "" {
				if lastRun, ok := lastHostRun[parsed.Host]; ok {
					waitFor := time.Duration(hostDelayMs)*time.Millisecond - time.Since(lastRun)
					if waitFor > 0 {
						time.Sleep(waitFor)
					}
				}
				lastHostRun[parsed.Host] = time.Now()
			}
		}

		requester(target, proxy, userAgent, reqHeaders, bypassIP, folder, httpMethod, verbose, technique, nobanner, rateLimit, timeout, redirect, randomAgent)
	}
}

// readURLsFromInput checks if the input is a file path containing URLs.
// If it is, returns all URLs from the file. Otherwise returns the input as a single URL.
func readURLsFromInput(input string) []string {
	// If input looks like a URL (has scheme), treat it as a single URL
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return []string{input}
	}

	// Try to open as a file
	file, err := os.Open(input)
	if err != nil {
		// Not a file, treat as a URL
		return []string{input}
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	if err := scanner.Err(); err != nil {
		log.Printf("[!] Error reading URL file: %v", err)
		return []string{input}
	}

	if len(urls) == 0 {
		return []string{input}
	}

	log.Printf("[*] Loaded %d URLs from %s", len(urls), input)
	return urls
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".nomore403" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".nomore403")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
