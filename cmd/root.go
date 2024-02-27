package cmd

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	bypassIP      string
	cfgFile       string
	delay         int
	folder        string
	httpMethod    string
	maxGoroutines int
	nobanner      bool
	proxy         string
	randomAgent   bool
	rateLimit     bool
	redirect      bool
	reqHeaders    []string
	requestFile   string
	schema        bool
	uri           string
	userAgent     string
	verbose       bool
)

// rootCmd
var rootCmd = &cobra.Command{
	Use:   "nomore403",
	Short: "Tool to bypass 40X response codes.",
	Long:  `Command line application that automates different ways to bypass 40X codes.`,

	Run: func(cmd *cobra.Command, args []string) {
		if len(folder) == 0 {
			folder = "payloads"
		}

		fi, _ := os.Stdin.Stat()
		if (fi.Mode() & os.ModeCharDevice) == 0 {
			bytes, _ := ioutil.ReadAll(os.Stdin)
			content := string(bytes)
			lines := strings.Split(content, "\n")
			lastchar := lines[len(lines)-1]
			for _, line := range lines {
				uri = line
				if uri == lastchar {
					break
				}
				requester(uri, proxy, userAgent, reqHeaders, bypassIP, folder, httpMethod, verbose, nobanner, rateLimit, redirect, randomAgent)
			}
		} else {
			if len(requestFile) > 0 {
				loadFlagsFromRequestFile(requestFile, schema, verbose, redirect)
			} else {
				if len(uri) == 0 {
					err := cmd.Help()
					if err != nil {
						log.Fatalf("Error printing help: %v", err)
					}
					log.Fatal()
				}
				requester(uri, proxy, userAgent, reqHeaders, bypassIP, folder, httpMethod, verbose, nobanner, rateLimit, redirect, randomAgent)
			}
		}
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cobra.CheckErr(rootCmd.Execute())
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVarP(&bypassIP, "bypass-ip", "i", "", "Try bypass tests with a specific IP address (or hostname). For example: 'X-Forwarded-For: 192.168.0.1' instead of 'X-Forwarded-For: 127.0.0.1'")
	rootCmd.PersistentFlags().IntVarP(&delay, "delay", "d", 0, "Set a delay (in ms) between each request (default 0ms)")
	rootCmd.PersistentFlags().StringVarP(&folder, "folder", "f", "", "Define payloads folder (if its not in the same path as binary)")
	rootCmd.PersistentFlags().StringSliceVarP(&reqHeaders, "header", "H", []string{""}, "Add a custom header to the requests (can be specified multiple times)")
	rootCmd.PersistentFlags().BoolVarP(&schema, "http", "", false, "Set HTTP schema for request-file requests (default HTTPS)")
	rootCmd.PersistentFlags().StringVarP(&httpMethod, "http-method", "t", "", "HTTP method to use (default 'GET')")
	rootCmd.PersistentFlags().IntVarP(&maxGoroutines, "max-goroutines", "m", 50, "Set the max number of goroutines working at same time")
	rootCmd.PersistentFlags().BoolVarP(&nobanner, "no-banner", "", false, "Set no-banner ON (default OFF)")
	rootCmd.PersistentFlags().StringVarP(&proxy, "proxy", "x", "", "Proxy URL. For example: http://127.0.0.1:8080")
	rootCmd.PersistentFlags().BoolVarP(&randomAgent, "random-agent", "", false, "Set random user-agent ON (default OFF)")
	rootCmd.PersistentFlags().BoolVarP(&rateLimit, "rate-limit", "l", false, "Stop making request if rate limit ban is detected: 429 HTTP code (default OFF)")
	rootCmd.PersistentFlags().BoolVarP(&redirect, "redirect", "r", false, "Set follow redirect ON (default OFF)")
	rootCmd.PersistentFlags().StringVarP(&requestFile, "request-file", "", "", "Path to request file to load flags from")
	rootCmd.PersistentFlags().StringVarP(&uri, "uri", "u", "", "Target URL")
	rootCmd.PersistentFlags().StringVarP(&userAgent, "user-agent", "a", "", "Set the User-Agent string (default 'nomore403')")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Set verbose mode ON (default OFF)")
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
		_, err := fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
		if err != nil {
			log.Fatalf("{#err}")
		}
	}
}
