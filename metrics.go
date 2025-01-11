package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

// MetricEvent represents a single event that we want to log
type MetricEvent struct {
	Timestamp time.Time
	UserID    string
	EventType string
	Details   map[string]interface{}
}

// MetricsManager handles the metrics collection and reporting with detailed logs
type MetricsManager struct {
	enabled   bool
	fileMutex sync.Mutex
	logs      []MetricEvent
	filePath  string
	ticker    *time.Ticker
	wg        sync.WaitGroup
	stopChan  chan struct{}
}

// NewMetricsManager initializes a new metrics manager
func NewMetricsManager(enabled bool, filePath string, interval time.Duration) *MetricsManager {
	mm := &MetricsManager{
		enabled:  enabled,
		logs:     []MetricEvent{},
		filePath: filePath,
		ticker:   time.NewTicker(interval),
		stopChan: make(chan struct{}),
	}

	if mm.enabled {
		mm.wg.Add(1)
		go mm.run()
	}

	return mm
}

func (mm *MetricsManager) run() {
	defer mm.wg.Done()
	for {
		select {
		case <-mm.ticker.C:
			mm.saveToFile()
		case <-mm.stopChan:
			mm.ticker.Stop()
			mm.saveToFile()
			return
		}
	}
}

// logEvent logs an event with its details
func (mm *MetricsManager) logEvent(userID, eventType string, details map[string]interface{}) {
	if !mm.enabled {
		return
	}

	event := MetricEvent{
		Timestamp: time.Now(),
		UserID:    userID,
		EventType: eventType,
		Details:   details,
	}

	mm.fileMutex.Lock()
	mm.logs = append(mm.logs, event)
	mm.fileMutex.Unlock()
}

// logRequest logs a user request
func (mm *MetricsManager) logRequest(userID string) {
	mm.logEvent(userID, "request", nil)
}

func (mm *MetricsManager) logFollow(userID string) {
	mm.logEvent(userID, "follow", nil)
}

// logSuccessfulGeneration logs a successful alt-text generation
func (mm *MetricsManager) logSuccessfulGeneration(userID, mediaType string, responseTimeMillis int64) {
	details := map[string]interface{}{
		"mediaType":    mediaType,
		"responseTime": responseTimeMillis,
	}
	mm.logEvent(userID, "successful_generation", details)
}

// logRateLimitHit logs when a rate limit is hit
func (mm *MetricsManager) logRateLimitHit(userID string) {
	mm.logEvent(userID, "rate_limit_hit", nil)
}

func (mm *MetricsManager) logNewAccountActivity(userID string) {
	mm.logEvent(userID, "new_account_activity", nil)
}

// logConsentRequest logs a consent request
func (mm *MetricsManager) logConsentRequest(userID string, granted bool) {
	details := map[string]interface{}{
		"granted": granted,
	}
	mm.logEvent(userID, "consent_request", details)
}

// saveToFile writes the current metrics data to a file
func (mm *MetricsManager) saveToFile() {
	mm.fileMutex.Lock()
	defer mm.fileMutex.Unlock()

	file, err := os.OpenFile(mm.filePath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Printf("Error opening metrics file: %v", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(mm.logs); err != nil {
		log.Printf("Error writing metrics to file: %v", err)
	}
}

// stop terminates the background metrics manager
func (mm *MetricsManager) stop() {
	close(mm.stopChan)
	mm.wg.Wait()
}
