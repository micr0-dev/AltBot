package main

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

// RunSetupWizard guides the user through setup and writes config to a file
func runSetupWizard(filePath string) {
	fmt.Println(Cyan + "Welcome to the AltBot Setup Wizard!" + Reset)

	// Load the default config
	if _, err := toml.DecodeFile("config.toml", &config); err != nil {
		log.Fatalf("Error loading config.toml: %v", err)
	}

	config.Server.MastodonServer = promptString(Blue+"Mastodon Server URL:"+Reset, config.Server.MastodonServer)
	config.Server.ClientSecret = promptString(Pink+"Mastodon Client Secret:"+Reset, config.Server.ClientSecret)
	config.Server.AccessToken = promptString(Green+"Mastodon Access Token:"+Reset, config.Server.AccessToken)
	config.Server.Username = promptString(Yellow+"Bot Username:"+Reset, config.Server.Username)

	config.RateLimit.AdminContactHandle = promptString(Red+"Admin Contact Handle:"+Reset, config.RateLimit.AdminContactHandle)

	config.RateLimit.Enabled = promptBool(Cyan+"Enable Rate Limiting (true/false)?"+Reset, fmt.Sprintf("%t", config.RateLimit.Enabled))
	config.WeeklySummary.Enabled = promptBool(Blue+"Enable Weekly Summary (true/false)?"+Reset, fmt.Sprintf("%t", config.WeeklySummary.Enabled))
	config.Metrics.Enabled = promptBool(Cyan+"Enable Metrics (true/false)?"+Reset, fmt.Sprintf("%t", config.Metrics.Enabled))
	config.Metrics.DashboardEnabled = promptBool(Blue+"Enable Metrics Dashboard (true/false)?"+Reset, fmt.Sprintf("%t", config.Metrics.DashboardEnabled))
	config.AltTextReminders.Enabled = promptBool(Cyan+"Enable Alt-Text Reminders (true/false)?"+Reset, fmt.Sprintf("%t", config.AltTextReminders.Enabled))

	saveConfig(filePath)

	fmt.Println(Green + "Configuration complete! Your settings have been saved to " + filePath + Reset)
}

// getStringInput prompts for a string input and returns the entered value or a default
func promptString(prompt, defaultValue string) string {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [%s]: ", prompt, defaultValue)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		return defaultValue
	}
	return input
}

// getBoolInput prompts for a boolean input and returns the boolean value
func promptBool(prompt, defaultValue string) bool {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Printf("%s [%s]: ", prompt, defaultValue)
		input, _ := reader.ReadString('\n')
		input = strings.ToLower(strings.TrimSpace(input))

		if input == "" {
			input = defaultValue
		}

		switch input {
		case "true", "t", "yes", "y":
			return true
		case "false", "f", "no", "n":
			return false
		default:
			fmt.Println(Red + "Please enter 'true' or 'false'." + Reset)
		}
	}
}

// saveConfig writes the config struct to a file named config.toml
func saveConfig(filePath string) {
	file, err := os.Create(filePath)
	if err != nil {
		log.Fatalf("Error creating config file: %v", err)
	}
	defer file.Close()

	encoder := toml.NewEncoder(file)
	if err := encoder.Encode(config); err != nil {
		log.Fatalf("Error encoding config to file: %v", err)
	}
}
