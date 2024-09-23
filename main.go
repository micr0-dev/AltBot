package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"github.com/mattn/go-mastodon"
	"github.com/nfnt/resize"
	"google.golang.org/api/option"
)

var model *genai.GenerativeModel
var ctx context.Context
var botAccountID mastodon.ID

func main() {
	// Load environment variables and set up Mastodon client
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	c := mastodon.NewClient(&mastodon.Config{
		Server:       os.Getenv("MASTODON_SERVER"),
		ClientID:     os.Getenv("MASTODON_CLIENT_ID"),
		ClientSecret: os.Getenv("MASTODON_CLIENT_SECRET"),
		AccessToken:  os.Getenv("MASTODON_ACCESS_TOKEN"),
	})

	// Fetch and verify the bot account ID
	botAccountID, err = fetchAndVerifyBotAccountID(c)
	if err != nil {
		log.Fatalf("Error fetching bot account ID: %v", err)
	}

	// Set up Gemini AI model
	err = Setup(os.Getenv("GEMINI_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	// Connect to Mastodon streaming API
	ws := c.NewWSClient()

	events, err := ws.StreamingWSUser(ctx)
	if err != nil {
		log.Fatalf("Error connecting to streaming API: %v", err)
	}

	fmt.Println("Connected to streaming API. All systems operational. Waiting for mentions and follows...")

	// Schedule periodic unfollow checks
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	go checkMutuals(ticker, c)

	// Main event loop
	for event := range events {
		switch e := event.(type) {
		case *mastodon.NotificationEvent:
			switch e.Notification.Type {
			case "mention":
				handleMention(c, e.Notification)
			case "follow":
				handleFollow(c, e.Notification)
			}
		case *mastodon.UpdateEvent:
			handleUpdate(c, e.Status)
		case *mastodon.ErrorEvent:
			log.Printf("Error event: %v", e.Error())
		case *mastodon.DeleteEvent:
			log.Printf("Delete event: status ID %v", e.ID)
		default:
			log.Printf("Unhandled event type: %T", e)
		}
	}
}

func checkMutuals(ticker *time.Ticker, c *mastodon.Client) {
	for range ticker.C {
		followers, err := getFollowers(c)
		if err != nil {
			log.Printf("Error fetching followers: %v", err)
		}

		following, err := getFollowing(c)
		if err != nil {
			log.Printf("Error fetching following: %v", err)
		}

		unfollowNonFollowers(c, followers, following)
		followBackMissedFollowers(c, followers, following)
	}
}

// fetchAndVerifyBotAccountID fetches and prints the bot account details to verify the account ID
func fetchAndVerifyBotAccountID(c *mastodon.Client) (mastodon.ID, error) {
	acct, err := c.GetAccountCurrentUser(ctx)
	if err != nil {
		return "", err
	}
	fmt.Printf("Bot Account ID: %s, Username: %s\n", acct.ID, acct.Acct)
	return acct.ID, nil
}

// Setup initializes the Gemini AI model with the provided API key
func Setup(apiKey string) error {
	ctx = context.Background()

	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return err
	}

	model = client.GenerativeModel("gemini-1.5-flash")

	model.SetTemperature(0.7)
	model.SetTopK(1)

	model.SafetySettings = []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockNone,
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockNone,
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockNone,
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockNone,
		},
	}

	return nil
}

// handleMention processes incoming mentions and generates alt-text descriptions
func handleMention(c *mastodon.Client, notification *mastodon.Notification) {
	if isDNI(&notification.Account) {
		return
	}

	originalStatus := notification.Status.InReplyToID
	if originalStatus == nil {
		return
	}

	var originalStatusID mastodon.ID

	switch id := originalStatus.(type) {
	case string:
		originalStatusID = mastodon.ID(id)
	case mastodon.ID:
		originalStatusID = id
	default:
		log.Printf("Unexpected type for InReplyToID: %T", originalStatus)
	}

	status, err := c.GetStatus(ctx, originalStatusID)
	if err != nil {
		log.Printf("Error fetching original status: %v", err)
		return
	}

	generateAndPostAltText(c, status, notification.Status.ID)
}

// isDNI checks if an account meets the Do Not Interact (DNI) conditions
func isDNI(account *mastodon.Account) bool {
	dniList := []string{
		"#nobot",
		"#noai",
		"#nollm",
	}

	if account.Acct == os.Getenv("MASTODON_USERNAME") {
		return true
	} else if account.Bot {
		return true
	}

	for _, tag := range dniList {
		if strings.Contains(account.Note, tag) {
			return true
		}
	}

	return false
}

// handleFollow processes new follows and follows back
func handleFollow(c *mastodon.Client, notification *mastodon.Notification) {
	_, err := c.AccountFollow(ctx, notification.Account.ID)
	if err != nil {
		log.Printf("Error following back: %v", err)
		return
	}
	fmt.Printf("Followed back: %s\n", notification.Account.Acct)
}

// unfollowNonFollowers unfollows accounts that are no longer following the bot
func unfollowNonFollowers(c *mastodon.Client, followers, following []mastodon.Account) {
	followerMap := make(map[mastodon.ID]bool)
	for _, follower := range followers {
		followerMap[follower.ID] = true
	}

	for _, followee := range following {
		if !followerMap[followee.ID] {
			_, err := c.AccountUnfollow(ctx, followee.ID)
			if err != nil {
				log.Printf("Error unfollowing %s: %v", followee.Acct, err)
			} else {
				fmt.Printf("Unfollowed: %s\n", followee.Acct)
			}
		}
	}
}

// followBackMissedFollowers follows back users who are following the bot but are not being followed by the bot
func followBackMissedFollowers(c *mastodon.Client, followers, following []mastodon.Account) {
	followingMap := make(map[mastodon.ID]bool)
	for _, followee := range following {
		followingMap[followee.ID] = true
	}

	for _, follower := range followers {
		if !followingMap[follower.ID] {
			_, err := c.AccountFollow(ctx, follower.ID)
			if err != nil {
				log.Printf("Error following back %s: %v", follower.Acct, err)
			} else {
				fmt.Printf("Followed back: %s\n", follower.Acct)
			}
		}
	}
}

// getFollowers returns a list of accounts following the bot
func getFollowers(c *mastodon.Client) ([]mastodon.Account, error) {
	var followers []mastodon.Account
	pg := mastodon.Pagination{Limit: 2000}
	for {
		fs, err := c.GetAccountFollowers(ctx, botAccountID, &pg)
		if err != nil {
			return nil, err
		}
		if len(fs) == 0 {
			break
		}
		for _, f := range fs {
			followers = append(followers, *f)
		}
		pg.MaxID = fs[len(fs)-1].ID
	}
	return followers, nil
}

// getFollowing returns a list of accounts the bot is following
func getFollowing(c *mastodon.Client) ([]mastodon.Account, error) {
	var following []mastodon.Account
	pg := mastodon.Pagination{Limit: 2000}
	for {
		fs, err := c.GetAccountFollowing(ctx, botAccountID, &pg)
		if err != nil {
			return nil, err
		}
		if len(fs) == 0 {
			break
		}
		for _, f := range fs {
			following = append(following, *f)
		}
		pg.MaxID = fs[len(fs)-1].ID
	}
	return following, nil
}

// handleUpdate processes new posts and generates alt-text descriptions if missing
func handleUpdate(c *mastodon.Client, status *mastodon.Status) {
	if status.Account.Acct == os.Getenv("MASTODON_USERNAME") {
		return
	}

	for _, attachment := range status.MediaAttachments {
		if attachment.Type == "image" && attachment.Description == "" {
			generateAndPostAltText(c, status, status.ID)
			break
		}
	}
}

// generateAndPostAltText generates alt-text for images and posts it as a reply
func generateAndPostAltText(c *mastodon.Client, status *mastodon.Status, replyToID mastodon.ID) {
	replyPost, err := c.GetStatus(ctx, replyToID)
	if err != nil {
		log.Printf("Error fetching reply status: %v", err)
		return
	}

	for _, attachment := range status.MediaAttachments {
		var response string
		if attachment.Type == "image" && attachment.Description == "" {
			altText, err := generateAltText(attachment.URL)
			if err != nil {
				log.Printf("Error generating alt-text: %v", err)
				altText = "Sorry, I couldn't process this image."
			}

			response = fmt.Sprintf("@%s %s", replyPost.Account.Acct, altText)

			if err != nil {
				log.Printf("Error posting alt-text: %v", err)
			} else {
				fmt.Printf("Posted alt-text: %s\n", response)
			}
		} else if attachment.Description != "" {
			response = fmt.Sprintf("@%s This image already has alt-text", replyPost.Account.Acct)
		} else {
			response = fmt.Sprintf("@%s This is not an image, only images are supported currently", replyPost.Account.Acct)
		}

		visibility := replyPost.Visibility

		if visibility == "public" {
			visibility = "unlisted"
		}

		_, err = c.PostStatus(ctx, &mastodon.Toot{
			Status:      response,
			InReplyToID: replyToID,
			Visibility:  visibility,
		})

		if err != nil {
			log.Printf("Error posting reply: %v", err)
		}
	}
}

// generateAltText generates alt-text for an image using Gemini AI
func generateAltText(imageURL string) (string, error) {
	resp, err := http.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	img, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Downscale the image to a smaller width (e.g., 800 pixels)
	downscaledImg, format, err := downscaleImage(img, 800)
	if err != nil {
		return "", err
	}

	prompt := "Generate an alt-text description, which is a description for people who can't see the image. Be detailed but dont go too indepth, just write about the main subjects: "

	fmt.Println("Processing image: " + imageURL)

	return GenerateAlt(prompt, downscaledImg, format)
}

// Generate creates a response using the Gemini AI model
func GenerateAlt(strPrompt string, image []byte, fileExtension string) (string, error) {
	var parts []genai.Part

	parts = append(parts, genai.Text(strPrompt))
	parts = append(parts, genai.ImageData(fileExtension, image))

	resp, err := model.GenerateContent(ctx, parts...)
	if err != nil {
		return "", err
	}
	return getResponse(resp), nil
}

// downscaleImage resizes the image to the specified width while maintaining the aspect ratio
// and converts it to PNG or JPEG if it is in a different format.
func downscaleImage(imgData []byte, width uint) ([]byte, string, error) {
	img, format, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, "", err
	}

	// Resize the image to the specified width while maintaining the aspect ratio
	resizedImg := resize.Resize(width, 0, img, resize.Lanczos3)

	// Convert the image to PNG or JPEG if it is in a different format
	var buf bytes.Buffer
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, resizedImg, nil)
		format = "jpeg"
	case "png":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	case "gif":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	case "bmp":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	case "tiff":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	case "webp":
		err = png.Encode(&buf, resizedImg)
		format = "png"
	default:
		return nil, "", fmt.Errorf("unsupported image format: %s", format)
	}

	if err != nil {
		return nil, "", err
	}

	return buf.Bytes(), format, nil
}

// getResponse extracts the text response from the AI model's output
func getResponse(resp *genai.GenerateContentResponse) string {
	var response string
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				str := fmt.Sprintf("%v", part)
				response += str
			}
		}
	}
	return response
}
