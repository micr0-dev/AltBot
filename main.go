package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"github.com/mattn/go-mastodon"
	"github.com/nfnt/resize"
	"google.golang.org/api/option"
)

var model *genai.GenerativeModel
var ctx context.Context

var consentRequests = make(map[mastodon.ID]mastodon.ID)

func main() {
	// Load environment variables and set up Mastodon client
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	err = loadLocalizations()
	if err != nil {
		log.Fatalf("Error loading localizations: %v", err)
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
	_, err = fetchAndVerifyBotAccountID(c)
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

	// Main event loop
	for event := range events {
		switch e := event.(type) {
		case *mastodon.NotificationEvent:
			switch e.Notification.Type {
			case "mention":
				if originalStatus := e.Notification.Status.InReplyToID; originalStatus != nil {
					var originalStatusID mastodon.ID
					switch id := originalStatus.(type) {
					case string:
						originalStatusID = mastodon.ID(id)
					case mastodon.ID:
						originalStatusID = id
					}

					getStatus, err := c.GetStatus(ctx, originalStatusID)

					if err != nil {
						handleMention(c, e.Notification)
					}

					veryOriginalStatus := getStatus.InReplyToID

					var veryOriginalStatusID mastodon.ID
					switch id := veryOriginalStatus.(type) {
					case string:
						veryOriginalStatusID = mastodon.ID(id)
					case mastodon.ID:
						veryOriginalStatusID = id
					}

					if _, ok := consentRequests[veryOriginalStatusID]; ok {
						handleConsentResponse(c, veryOriginalStatusID, e.Notification.Status)
					} else {
						handleMention(c, e.Notification)
					}
				} else {
					handleMention(c, e.Notification)
				}
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

	//Check if the original status has any media attachments
	if len(status.MediaAttachments) == 0 {
		return
	}

	// Check if the person who mentioned the bot is the OP
	if status.Account.ID == notification.Account.ID {
		generateAndPostAltText(c, status, notification.Status.ID)
	} else {
		requestConsent(c, status, notification)
	}
}

// requestConsent asks the original poster for consent to generate alt text
func requestConsent(c *mastodon.Client, status *mastodon.Status, notification *mastodon.Notification) {
	consentRequests[status.ID] = notification.Status.ID

	message := fmt.Sprintf("@%s"+getLocalizedString(notification.Status.Language, "consentRequest", "response"), status.Account.Acct, notification.Account.Acct)
	_, err := c.PostStatus(ctx, &mastodon.Toot{
		Status:      message,
		InReplyToID: status.ID,
		Visibility:  status.Visibility,
		Language:    notification.Status.Language,
	})
	if err != nil {
		log.Printf("Error posting consent request: %v", err)
	}
}

// handleConsentResponse processes the consent response from the original poster
func handleConsentResponse(c *mastodon.Client, ID mastodon.ID, consentStatus *mastodon.Status) {
	originalStatusID := ID
	status, err := c.GetStatus(ctx, originalStatusID)
	if err != nil {
		log.Printf("Error fetching original status: %v", err)
		return
	}

	content := strings.TrimSpace(strings.ToLower(consentStatus.Content))
	if strings.Contains(content, "y") || strings.Contains(content, "yes") {
		generateAndPostAltText(c, status, consentStatus.ID)
	} else {
		log.Printf("Consent denied by the original poster: %s", consentStatus.Account.Acct)
	}
	delete(consentRequests, originalStatusID)

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
			altText, err := generateAltText(attachment.URL, replyPost.Language)
			if err != nil {
				log.Printf("Error generating alt-text: %v", err)
				altText = getLocalizedString(replyPost.Language, "altTextError", "response")
			}

			response = fmt.Sprintf("@%s %s", replyPost.Account.Acct, altText)

			if err != nil {
				log.Printf("Error posting alt-text: %v", err)
			} else {
				fmt.Printf("Posted alt-text: %s\n", response)
			}
		} else if attachment.Description != "" {
			response = fmt.Sprintf("@%s %s", replyPost.Account.Acct, getLocalizedString(replyPost.Language, "imageAlreadyHasAltText", "response"))
		} else {
			response = fmt.Sprintf("@%s %s", replyPost.Account.Acct, getLocalizedString(replyPost.Language, "notAnImage", "response"))
		}

		visibility := replyPost.Visibility

		if visibility == "public" {
			visibility = "unlisted"
		}

		_, err = c.PostStatus(ctx, &mastodon.Toot{
			Status:      response,
			InReplyToID: replyToID,
			Visibility:  visibility,
			Language:    replyPost.Language,
		})

		if err != nil {
			log.Printf("Error posting reply: %v", err)
		}
	}
}

// generateAltText generates alt-text for an image using Gemini AI
func generateAltText(imageURL string, lang string) (string, error) {
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

	prompt := getLocalizedString(lang, "generateAltText", "prompt")

	fmt.Println("Processing image: " + imageURL)

	return GenerateAlt(prompt, downscaledImg, format)
}

// Generate creates a response using the Gemini AI model
func GenerateAlt(strPrompt string, image []byte, fileExtension string) (string, error) {
	var parts []genai.Part

	parts = append(parts, genai.Text(strPrompt))
	parts = append(parts, genai.ImageData(fileExtension, image))

	fmt.Println("Generating content...")

	resp, err := model.GenerateContent(ctx, parts...)
	if err != nil {
		return "", err
	}
	return getResponse(resp), nil
}

// downscaleImage resizes the image to the specified width while maintaining the aspect ratio
// and converts it to PNG or JPEG if it is in a different format.
func downscaleImage(imgData []byte, width uint) ([]byte, string, error) {
	img, format, err := decodeImage(imgData)
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

// decodeImage decodes an image from bytes and returns the image and its format
func decodeImage(imgData []byte) (image.Image, string, error) {
	img, format, err := image.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, format, nil
	}

	// Try decoding as WebP if the standard decoding fails
	img, err = webp.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, "webp", nil
	}

	// Try decoding as BMP if the previous decodings fail
	img, err = bmp.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, "bmp", nil
	}

	// Try decoding as TIFF if the previous decodings fail
	img, err = tiff.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, "tiff", nil
	}

	// Try decoding as GIF if the previous decodings fail
	img, err = gif.Decode(bytes.NewReader(imgData))
	if err == nil {
		return img, "gif", nil
	}

	return nil, "", fmt.Errorf("unsupported image format: %v", err)
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
