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

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"github.com/mattn/go-mastodon"
	"github.com/nfnt/resize"
	"google.golang.org/api/option"
)

var model *genai.GenerativeModel
var ctx context.Context

func main() {
	// Load environment variables and set up Mastodon client
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	c := mastodon.NewClient(&mastodon.Config{
		Server:       os.Getenv("MASTODON_SERVER"),
		ClientID:     os.Getenv("MASTODON_CLIENT_ID"),
		ClientSecret: os.Getenv("MASTODON_CLIENT_SECRET"),
		AccessToken:  os.Getenv("MASTODON_ACCESS_TOKEN"),
	})

	// Set up Gemini AI model
	err = Setup(os.Getenv("GEMINI_API_KEY"))
	if err != nil {
		log.Fatal(err)
	}

	// Connect to Mastodon streaming API
	ws := c.NewWSClient()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events, err := ws.StreamingWSUser(ctx)
	if err != nil {
		log.Fatalf("Error connecting to streaming API: %v", err)
	}

	fmt.Println("Connected to streaming API. All systems operational. Waiting for mentions and follows...")

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
	// Ignore mentions from self
	if notification.Account.Acct == os.Getenv("MASTODON_USERNAME") {
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

// handleFollow processes new follows and follows back
func handleFollow(c *mastodon.Client, notification *mastodon.Notification) {
	_, err := c.AccountFollow(ctx, notification.Account.ID)
	if err != nil {
		log.Printf("Error following back: %v", err)
		return
	}
	fmt.Printf("Followed back: %s\n", notification.Account.Acct)
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

			response := fmt.Sprintf("@%s %s", replyPost.Account.Acct, altText)

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
		_, err = c.PostStatus(ctx, &mastodon.Toot{
			Status:      response,
			InReplyToID: replyToID,
			Visibility:  replyPost.Visibility,
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
func downscaleImage(imgData []byte, width uint) ([]byte, string, error) {
	img, format, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, "", err
	}

	// Resize the image to the specified width while maintaining the aspect ratio
	resizedImg := resize.Resize(width, 0, img, resize.Lanczos3)

	// Encode the resized image back to bytes
	var buf bytes.Buffer
	switch format {
	case "jpeg":
		err = jpeg.Encode(&buf, resizedImg, nil)
	case "png":
		err = png.Encode(&buf, resizedImg)
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
