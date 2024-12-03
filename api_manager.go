package main

import (
	"log"
	"sync"
	"time"
)

type APIKey struct {
	Key        string
	ProUsage   int
	FlashUsage int
	LastReset  time.Time
}

type APIManager struct {
	keys  []APIKey
	mutex sync.Mutex
}

func NewAPIManager(motherKey string) *APIManager {
	return &APIManager{
		keys: []APIKey{
			{Key: motherKey, LastReset: time.Now()},
		}}
}

func (m *APIManager) AddKey(key string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.keys = append(m.keys, APIKey{Key: key, LastReset: time.Now()})
	log.Printf("Added new API key: %s", key)
}

func (m *APIManager) GetAPIKey() (string, string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for i := 0; i < len(m.keys); i++ {
		key := &m.keys[i]

		if time.Since(key.LastReset) >= time.Minute {
			key.ProUsage = 0
			key.FlashUsage = 0
			key.LastReset = time.Now()
			log.Printf("Reset usage for API key: %s", key.Key)
		}

		if key.ProUsage < 2 {
			key.ProUsage++
			log.Printf("Using gemini-pro-1.5 with API key: %s (Pro usage: %d)", key.Key, key.ProUsage)
			return key.Key, "gemini-pro-1.5"
		}

	}

	for i := 0; i < len(m.keys); i++ {
		key := &m.keys[i]

		if key.FlashUsage < 15 {
			key.FlashUsage++
			log.Printf("Using gemini-flash-1.5 with API key: %s (Flash usage: %d)", key.Key, key.FlashUsage)
			return key.Key, "gemini-flash-1.5"
		}

	}

	log.Println("All API keys exhausted")
	return "", ""
}
