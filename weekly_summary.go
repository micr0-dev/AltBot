package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mattn/go-mastodon"
)

type WeeklySummary struct {
	AltTextCount int
	NewUserCount int
}

func GenerateWeeklySummary(c *mastodon.Client, ctx context.Context) {
	if !config.WeeklySummary.Enabled {
		return
	}

	// Fetch data for the past week
	summary := fetchWeeklyData()

	// Calculate leaderboard
	entries, err := readLogEntries()
	if err != nil {
		log.Printf("Error reading log entries: %v", err)
		return
	}
	userScores := calculateLeaderboard(entries)
	topUsers := getTopUsers(userScores)

	// Format leaderboard
	var leaderboardBuilder strings.Builder
	for _, user := range topUsers {
		leaderboardBuilder.WriteString(user + "\n")
	}
	leaderboard := leaderboardBuilder.String()

	// Select a random tip from the list
	tipOfTheWeek := config.WeeklySummary.Tips[rand.Intn(len(config.WeeklySummary.Tips))]

	// Create the summary message using the template
	message := strings.ReplaceAll(config.WeeklySummary.MessageTemplate, "{{alt_text_count}}", fmt.Sprintf("%d", summary.AltTextCount))
	message = strings.ReplaceAll(message, "{{new_user_count}}", fmt.Sprintf("%d", summary.NewUserCount))
	message = strings.ReplaceAll(message, "{{tip_of_the_week}}", tipOfTheWeek)
	message = strings.ReplaceAll(message, "{{leaderboard}}", leaderboard)

	// Post the summary
	post, err := c.PostStatus(ctx, &mastodon.Toot{
		Status:     message,
		Visibility: "public",
	})
	if err != nil {
		log.Printf("Error posting weekly summary: %v", err)
	} else {
		log.Printf("Weekly summary posted! \nLink: %s", post.URL)
	}
}

func calculateLeaderboard(entries []LogEntry) map[string]int {
	userScores := make(map[string]int)

	for _, entry := range entries {
		if entry.EventType == "human_written_alt_text" {
			userScores[entry.Username]++
		}
	}

	return userScores
}

func getTopUsers(userScores map[string]int) []string {
	type userScore struct {
		Username string
		Score    int
	}

	var scores []userScore
	for user, score := range userScores {
		scores = append(scores, userScore{Username: user, Score: score})
	}

	sort.Slice(scores, func(i, j int) bool {
		return scores[i].Score > scores[j].Score
	})

	var topUsers []string
	for i := 0; i < len(scores) && i < 3; i++ {
		topUsers = append(topUsers, fmt.Sprintf("%d. @%s (%d alt-texts)", i+1, scores[i].Username, scores[i].Score))
	}

	return topUsers
}

func startWeeklySummaryScheduler(c *mastodon.Client) {
	for {
		now := time.Now()
		// Calculate the next scheduled time based on config
		nextScheduledTime := calculateNextScheduledTime(now)
		durationUntilNext := nextScheduledTime.Sub(now)

		time.Sleep(1 * time.Second)
		fmt.Printf("Next weekly summary scheduled for %s\n", nextScheduledTime.Format("2006-01-02 15:04:05"))

		// Sleep until the next scheduled time
		time.Sleep(durationUntilNext)

		// Generate and post the weekly summary
		GenerateWeeklySummary(c, ctx)

		time.Sleep(5 * time.Second)
	}
}

func calculateNextScheduledTime(now time.Time) time.Time {
	// Parse the configured post day and time
	postDay := parseDayOfWeek(config.WeeklySummary.PostDay)
	postTime, _ := time.Parse("15:04", config.WeeklySummary.PostTime)

	// Calculate the next occurrence of the configured day and time
	nextScheduledTime := time.Date(now.Year(), now.Month(), now.Day(), postTime.Hour(), postTime.Minute(), 0, 0, now.Location())
	for nextScheduledTime.Weekday() != postDay || nextScheduledTime.Before(now) {
		nextScheduledTime = nextScheduledTime.AddDate(0, 0, 1)
	}

	return nextScheduledTime
}

func parseDayOfWeek(day string) time.Weekday {
	switch strings.ToLower(day) {
	case "sunday":
		return time.Sunday
	case "monday":
		return time.Monday
	case "tuesday":
		return time.Tuesday
	case "wednesday":
		return time.Wednesday
	case "thursday":
		return time.Thursday
	case "friday":
		return time.Friday
	case "saturday":
		return time.Saturday
	default:
		return time.Sunday // Default to Sunday if parsing fails
	}
}

func fetchWeeklyData() WeeklySummary {
	entries, err := readLogEntries()
	if err != nil {
		log.Printf("Error reading log entries: %v", err)
		return WeeklySummary{}
	}

	oneWeekAgo := time.Now().AddDate(0, 0, -7)
	altTextCount := 0
	newUserCount := 0

	for _, entry := range entries {
		if entry.Timestamp.After(oneWeekAgo) {
			switch entry.EventType {
			case "alt_text_generated":
				altTextCount++
			case "new_follower":
				newUserCount++
			}
		}
	}

	return WeeklySummary{
		AltTextCount: altTextCount,
		NewUserCount: newUserCount,
	}
}

func readLogEntries() ([]LogEntry, error) {
	file, err := os.Open("altbot_log.json")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var entry LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			log.Printf("Error decoding log entry: %v", err)
			continue
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

// LogEntry represents a log entry for an event
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	EventType string    `json:"event_type"`
	Username  string    `json:"username,omitempty"`
}

func LogEvent(eventType string) {
	if !config.WeeklySummary.Enabled {
		return
	}
	entry := LogEntry{
		Timestamp: time.Now(),
		EventType: eventType,
	}

	file, err := os.OpenFile("altbot_log.json", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening log file: %v", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	if err := encoder.Encode(entry); err != nil {
		log.Printf("Error writing log entry: %v", err)
	}
}

func LogEventWithUsername(eventType, username string) {
	if !config.WeeklySummary.Enabled {
		return
	}
	entry := LogEntry{
		Timestamp: time.Now(),
		EventType: eventType,
		Username:  username,
	}

	file, err := os.OpenFile("altbot_log.json", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("Error opening log file: %v", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	if err := encoder.Encode(entry); err != nil {
		log.Printf("Error writing log entry: %v", err)
	}
}
