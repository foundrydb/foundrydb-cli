package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	foundrydb "github.com/anorph/foundrydb-sdk-go/foundrydb"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile  string
	apiURL   string
	username string
	password string
	orgID    string
	jsonOut  bool
)

// rootCmd is the base command
var rootCmd = &cobra.Command{
	Use:     "fdb",
	Short:   "fdb - CLI for FoundryDB managed database platform",
	Version: "0.1.0",
	Long: `fdb is a command-line interface for managing databases on the FoundryDB platform.

It allows you to create, inspect, and manage PostgreSQL, MySQL, MongoDB,
Valkey, Kafka, OpenSearch, and MSSQL services through a simple CLI.`,
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.fdb/config.toml)")
	rootCmd.PersistentFlags().StringVar(&apiURL, "api-url", "", "FoundryDB API base URL (default: https://api.foundrydb.com)")
	rootCmd.PersistentFlags().StringVar(&username, "username", "", "API username (default: admin)")
	rootCmd.PersistentFlags().StringVar(&password, "password", "", "API password")
	rootCmd.PersistentFlags().StringVar(&orgID, "org", "", "Organization UUID or slug (sets X-Active-Org-ID header)")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "Output raw JSON instead of formatted tables")

	viper.BindPFlag("api_url", rootCmd.PersistentFlags().Lookup("api-url"))
	viper.BindPFlag("username", rootCmd.PersistentFlags().Lookup("username"))
	viper.BindPFlag("password", rootCmd.PersistentFlags().Lookup("password"))
	viper.BindPFlag("org", rootCmd.PersistentFlags().Lookup("org"))

	rootCmd.AddCommand(authCmd)
	rootCmd.AddCommand(servicesCmd)
	rootCmd.AddCommand(appServiceCmd)
	rootCmd.AddCommand(orgCmd)
	rootCmd.AddCommand(usersCmd)
	rootCmd.AddCommand(backupsCmd)
	rootCmd.AddCommand(connectCmd)
	rootCmd.AddCommand(connectionStringCmd)
	rootCmd.AddCommand(logsCmd)
	rootCmd.AddCommand(metricsCmd)
	rootCmd.AddCommand(presetsCmd)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			configDir := filepath.Join(home, ".fdb")
			viper.AddConfigPath(configDir)
			viper.SetConfigName("config")
			viper.SetConfigType("toml")
		}
	}

	viper.SetEnvPrefix("FDB")
	viper.AutomaticEnv()

	// Defaults
	viper.SetDefault("api_url", "https://api.foundrydb.com")
	viper.SetDefault("username", "admin")

	viper.ReadInConfig()
}

// newClient creates an API client from current config/flags
func newClient() *foundrydb.Client {
	url := viper.GetString("api_url")
	user := viper.GetString("username")
	pass := viper.GetString("password")
	org := viper.GetString("org")

	// Flag overrides
	if apiURL != "" {
		url = apiURL
	}
	if username != "" {
		user = username
	}
	if password != "" {
		pass = password
	}
	if orgID != "" {
		org = orgID
	}

	return foundrydb.New(foundrydb.Config{
		APIURL:   url,
		Username: user,
		Password: pass,
		OrgID:    org,
	})
}

// getConfigPath returns the path to the config file
func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, ".fdb", "config.toml"), nil
}
