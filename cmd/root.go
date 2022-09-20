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
	cfgFile        string
	uri            string
	proxy          string
	useragent      string
	max_goroutines int
	delay          int
	req_headers    []string
	bypassIp       string
	folder         string
	httpMethod     string
)

// rootCmd
var rootCmd = &cobra.Command{
	Use:   "dontgo403",
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
			lines := strings.Split(string(content), "\n")
			lastchar := (lines[len(lines)-1])
			for _, line := range lines {
				uri = line
				if uri == lastchar {
					break
				}
				requester(uri, proxy, useragent, req_headers, bypassIp, folder, httpMethod)
			}
		} else {
			if len(uri) == 0 {
				cmd.Help()
				log.Fatal()
			}
			requester(uri, proxy, useragent, req_headers, bypassIp, folder, httpMethod)
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

	rootCmd.PersistentFlags().StringVarP(&uri, "uri", "u", "", "Target URL")
	rootCmd.PersistentFlags().StringVarP(&proxy, "proxy", "p", "", "Proxy URL. For example: http://127.0.0.1:8080")
	rootCmd.PersistentFlags().StringVarP(&useragent, "useragent", "a", "", "Set the User-Agent string (default 'dontgo403')")
	rootCmd.PersistentFlags().IntVarP(&max_goroutines, "max_goroutines", "m", 50, "Set the max number of goroutines working at same time. Default: 50")
	rootCmd.PersistentFlags().IntVarP(&delay, "delay", "d", 0, "Set a delay (in ms) between each request. Default: 0ms")
	rootCmd.PersistentFlags().StringSliceVarP(&req_headers, "header", "H", []string{""}, "Add a custom header to the requests (can be specified multiple times)")
	rootCmd.PersistentFlags().StringVarP(&bypassIp, "bypassIp", "b", "", "Try bypass tests with a specific IP address (or hostname). i.e.: 'X-Forwarded-For: 192.168.0.1' instead of 'X-Forwarded-For: 127.0.0.1'")
	rootCmd.PersistentFlags().StringVarP(&folder, "folder", "f", "", "Define payloads folder (if it's not in the same path as binary)")
	rootCmd.PersistentFlags().StringVarP(&httpMethod, "httpMethod", "t", "", "HTTP method to use (default 'GET')")
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

		// Search config in home directory with name ".dontgo403" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigType("yaml")
		viper.SetConfigName(".dontgo403")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
