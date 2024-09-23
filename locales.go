package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Localization holds the localized strings for different languages
type Localization struct {
	Prompts   map[string]string `json:"prompts"`
	Responses map[string]string `json:"responses"`
}

var localizations map[string]Localization

func loadLocalizations() error {
	data, err := os.ReadFile("localizations.json")
	if err != nil {
		return err
	}

	err = json.Unmarshal(data, &localizations)
	if err != nil {
		return err
	}

	return nil
}

func getLocalizedString(lang, key string, category string) string {
	fmt.Println(lang, key, category)
	localization := localizations["en"]
	if value, ok := localizations[lang]; ok {
		localization = value
	}
	fmt.Println(localization)

	switch category {
	case "prompt":
		if value, ok := localization.Prompts[key]; ok {
			return value
		}
	case "response":
		if value, ok := localization.Responses[key]; ok {
			return value
		}
	}
	return ""
}
