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
	timeout       int
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
				requester(uri, proxy, userAgent, reqHeaders, bypassIP, folder, httpMethod, verbose, nobanner, rateLimit, timeout, redirect, randomAgent)
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
				requester(uri, proxy, userAgent, reqHeaders, bypassIP, folder, httpMethod, verbose, nobanner, rateLimit, timeout, redirect, randomAgent)
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

	rootCmd.PersistentFlags().StringVarP(&bypassIP, "bypass-ip", "i", "", "Use a specified IP address or hostname for bypassing access controls. Injects this IP in headers like 'X-Forwarded-For'.")
	rootCmd.PersistentFlags().IntVarP(&delay, "delay", "d", 0, "Specify a delay between requests in milliseconds. Helps manage request rate (default: 0ms).")
	rootCmd.PersistentFlags().StringVarP(&folder, "folder", "f", "", "Specify the folder location for payloads if not in the same directory as the executable.")
	rootCmd.PersistentFlags().StringSliceVarP(&reqHeaders, "header", "H", []string{""}, "Add one or more custom headers to requests. Repeatable flag for multiple headers.")
	rootCmd.PersistentFlags().BoolVarP(&schema, "http", "", false, "Use HTTP instead of HTTPS for requests defined in the request file.")
	rootCmd.PersistentFlags().StringVarP(&httpMethod, "http-method", "t", "", "Specify the HTTP method for the request (e.g., GET, POST). Default is 'GET'.")
	rootCmd.PersistentFlags().IntVarP(&maxGoroutines, "max-goroutines", "m", 50, "Limit the maximum number of concurrent goroutines to manage load (default: 50).")
	rootCmd.PersistentFlags().BoolVarP(&nobanner, "no-banner", "", false, "Disable the display of the startup banner (default: banner shown).")
	rootCmd.PersistentFlags().StringVarP(&proxy, "proxy", "x", "", "Specify a proxy server for requests, e.g., 'http://server:port'.")
	rootCmd.PersistentFlags().BoolVarP(&randomAgent, "random-agent", "", false, "Enable the use of a randomly selected User-Agent.")
	rootCmd.PersistentFlags().BoolVarP(&rateLimit, "rate-limit", "l", false, "Halt requests upon encountering a 429 (rate limit) HTTP status code.")
	rootCmd.PersistentFlags().BoolVarP(&redirect, "redirect", "r", false, "Automatically follow redirects in responses.")
	rootCmd.PersistentFlags().StringVarP(&requestFile, "request-file", "", "", "Load request configuration and flags from a specified file.")
	rootCmd.PersistentFlags().IntVarP(&timeout, "timeout", "", 6000, "Specify a max timeout time (default: 6000ms).")
	rootCmd.PersistentFlags().StringVarP(&uri, "uri", "u", "", "Specify the target URL for the request.")
	rootCmd.PersistentFlags().StringVarP(&userAgent, "user-agent", "a", "", "pecify a custom User-Agent string for requests (default: 'nomore403').")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output for detailed request/response logging.")
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
