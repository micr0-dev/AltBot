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
	"os/exec"
	"strings"

	"github.com/BurntSushi/toml"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
	"golang.org/x/image/webp"

	"github.com/google/generative-ai-go/genai"
	"github.com/mattn/go-mastodon"
	"github.com/nfnt/resize"
	"google.golang.org/api/option"
)

// Version of the bot
const Version = "1.2.0"

// AsciiArt is the ASCII art for the bot
const AsciiArt = `    _   _ _   ___     _   
   /_\ | | |_| _ )___| |_ 
  / _ \| |  _| _ / _ |  _|
 /_/ \_|_|\__|___\___/\__|
`

type Config struct {
	Server struct {
		MastodonServer string `toml:"mastodon_server"`
		ClientSecret   string `toml:"client_secret"`
		AccessToken    string `toml:"access_token"`
		Username       string `toml:"username"`
	} `toml:"server"`
	LLM struct {
		Provider    string `toml:"provider"`
		OllamaModel string `toml:"ollama_model"`
	} `toml:"llm"`
	Gemini struct {
		APIKey      string  `toml:"api_key"`
		Temperature float32 `toml:"temperature"`
		TopK        int32   `toml:"top_k"`
	} `toml:"gemini"`
	SafetySettings struct {
		HarassmentThreshold       string `toml:"harassment_threshold"`
		HateSpeechThreshold       string `toml:"hate_speech_threshold"`
		SexuallyExplicitThreshold string `toml:"sexually_explicit_threshold"`
		DangerousContentThreshold string `toml:"dangerous_content_threshold"`
	} `toml:"safety_settings"`
	Localization struct {
		DefaultLanguage string `toml:"default_language"`
	} `toml:"localization"`
	DNI struct {
		Tags       []string `toml:"tags"`
		IgnoreBots bool     `toml:"ignore_bots"`
	} `toml:"dni"`
	ImageProcessing struct {
		DownscaleWidth uint `toml:"downscale_width"`
	} `toml:"image_processing"`
	Behavior struct {
		ReplyVisibility string `toml:"reply_visibility"`
		FollowBack      bool   `toml:"follow_back"`
		AskForConsent   bool   `toml:"ask_for_consent"`
	} `toml:"behavior"`
	WeeklySummary struct {
		Enabled         bool     `toml:"enabled"`
		PostDay         string   `toml:"post_day"`
		PostTime        string   `toml:"post_time"`
		MessageTemplate string   `toml:"message_template"`
		Tips            []string `toml:"tips"`
	} `toml:"weekly_summary"`
}

var config Config
var model *genai.GenerativeModel
var ctx context.Context

var consentRequests = make(map[mastodon.ID]mastodon.ID)

func main() {
	// Load configuration from TOML file
	if _, err := toml.DecodeFile("config.toml", &config); err != nil {
		log.Fatalf("Error loading config.toml: %v", err)
	}

	if config.Server.MastodonServer == "https://mastodon.example.com" {
		log.Fatal("Please configure the Mastodon server in config.toml")
	}

	if config.LLM.Provider == "ollama" {
		err := checkOllamaModel()
		if err != nil {
			log.Fatalf("Error checking Ollama model: %v", err)
		}
	}

	err := loadLocalizations()
	if err != nil {
		log.Fatalf("Error loading localizations: %v", err)
	}

	// Print the version and art
	fmt.Print(AsciiArt)
	fmt.Printf("AltBot v%s (%s)\n", Version, config.LLM.Provider)

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()

	c := mastodon.NewClient(&mastodon.Config{
		Server:       config.Server.MastodonServer,
		ClientSecret: config.Server.ClientSecret,
		AccessToken:  config.Server.AccessToken,
	})

	// Fetch and verify the bot account ID
	_, err = fetchAndVerifyBotAccountID(c)
	if err != nil {
		log.Fatalf("Error fetching bot account ID: %v", err)
	}

	// Set up Gemini AI model
	err = Setup(config.Gemini.APIKey)
	if err != nil {
		log.Fatal(err)
	}

	// Connect to Mastodon streaming API
	ws := c.NewWSClient()

	events, err := ws.StreamingWSUser(ctx)
	if err != nil {
		log.Fatalf("Error connecting to streaming API: %v", err)
	}

	if config.WeeklySummary.Enabled {
		go startWeeklySummaryScheduler(c)
	}

	fmt.Println("Connected to streaming API. All systems operational. Waiting for mentions and follows...")

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
	fmt.Printf("Bot Account ID: %s, Username: %s\n\n", acct.ID, acct.Acct)
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

	model.SetTemperature(config.Gemini.Temperature)
	model.SetTopK(config.Gemini.TopK)

	model.SafetySettings = []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: mapHarmBlock(config.SafetySettings.HarassmentThreshold),
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: mapHarmBlock(config.SafetySettings.HateSpeechThreshold),
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: mapHarmBlock(config.SafetySettings.SexuallyExplicitThreshold),
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: mapHarmBlock(config.SafetySettings.DangerousContentThreshold),
		},
	}

	return nil
}

// mapHarmBlock maps the TOML string values to the genai package constants
func mapHarmBlock(threshold string) genai.HarmBlockThreshold {
	switch threshold {
	case "none":
		return genai.HarmBlockNone
	case "low":
		return genai.HarmBlockLowAndAbove
	case "medium":
		return genai.HarmBlockMediumAndAbove
	case "high":
		return genai.HarmBlockOnlyHigh
	default:
		return genai.HarmBlockNone
	}
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
	} else if !config.Behavior.AskForConsent {
		generateAndPostAltText(c, status, notification.Status.ID)
	} else {
		requestConsent(c, status, notification)
	}
}

// requestConsent asks the original poster for consent to generate alt text
func requestConsent(c *mastodon.Client, status *mastodon.Status, notification *mastodon.Notification) {
	// Check if every image in the post already has a Alt text
	hasAltText := true

	for _, attachment := range status.MediaAttachments {
		if attachment.Type == "image" && attachment.Description == "" {
			hasAltText = false
		}
	}

	if hasAltText {
		return
	}

	// Check if the original poster has already been asked for consent
	if _, ok := consentRequests[status.ID]; ok {
		return
	}

	consentRequests[status.ID] = notification.Status.ID

	message := fmt.Sprintf("@%s "+getLocalizedString(notification.Status.Language, "consentRequest", "response"), status.Account.Acct, notification.Account.Acct)
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
	dniList := config.DNI.Tags

	if account.Acct == config.Server.Username {
		return true
	} else if account.Bot && config.DNI.IgnoreBots {
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
	if config.Behavior.FollowBack {
		_, err := c.AccountFollow(ctx, notification.Account.ID)
		if err != nil {
			log.Printf("Error following back: %v", err)
			return
		}
		LogEvent("new_follower")
		fmt.Printf("Followed back: %s\n", notification.Account.Acct)
	}
}

// handleUpdate processes new posts and generates alt-text descriptions if missing
func handleUpdate(c *mastodon.Client, status *mastodon.Status) {
	if status.Account.Acct == config.Server.Username {
		return
	}

	for _, attachment := range status.MediaAttachments {
		if attachment.Type == "image" {
			if attachment.Description == "" {
				generateAndPostAltText(c, status, status.ID)
				break
			} else {
				LogEventWithUsername("human_written_alt_text", status.Account.Acct)
			}
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

	var responses []string
	altTextGenerated := false
	altTextAlreadyExists := false

	for _, attachment := range status.MediaAttachments {
		if attachment.Type == "image" {
			if attachment.Description == "" {
				altText, err := generateAltText(attachment.URL, replyPost.Language)
				if err != nil {
					log.Printf("Error generating alt-text: %v", err)
					altText = getLocalizedString(replyPost.Language, "altTextError", "response")
				} else if altText == "" {
					log.Printf("Error generating alt-text: Empty response")
					altText = getLocalizedString(replyPost.Language, "altTextError", "response")
				}
				responses = append(responses, fmt.Sprintf("@%s %s", replyPost.Account.Acct, altText))
				altTextGenerated = true
			} else if !altTextGenerated && !altTextAlreadyExists {
				responses = append(responses, fmt.Sprintf("@%s %s", replyPost.Account.Acct, getLocalizedString(replyPost.Language, "imageAlreadyHasAltText", "response")))
				altTextAlreadyExists = true
			}
		} else {
			responses = append(responses, fmt.Sprintf("@%s %s", replyPost.Account.Acct, getLocalizedString(replyPost.Language, "notAnImage", "response")))
		}
	}

	// Prepare the content warning for the reply
	contentWarning := status.SpoilerText
	if contentWarning != "" && !strings.HasPrefix(contentWarning, "re:") {
		contentWarning = "re: " + contentWarning
	}

	// Post all accumulated responses
	for _, response := range responses {
		visibility := replyPost.Visibility

		// Map the visibility of the reply based on the original post and the bot's settings
		switch strings.ToLower(config.Behavior.ReplyVisibility + "," + replyPost.Visibility) {
		case "public,public":
			visibility = "public"
		case "public,unlisted":
			visibility = "unlisted"
		case "public,private":
			visibility = "private"
		case "public,direct":
			visibility = "direct"
		case "unlisted,public":
			visibility = "unlisted"
		case "unlisted,unlisted":
			visibility = "unlisted"
		case "unlisted,private":
			visibility = "private"
		case "unlisted,direct":
			visibility = "direct"
		case "private,public":
			visibility = "private"
		case "private,unlisted":
			visibility = "private"
		case "private,private":
			visibility = "private"
		case "private,direct":
			visibility = "direct"
		case "direct,public":
			visibility = "direct"
		case "direct,unlisted":
			visibility = "direct"
		case "direct,private":
			visibility = "direct"
		case "direct,direct":
			visibility = "direct"
		}

		_, err = c.PostStatus(ctx, &mastodon.Toot{
			Status:      response,
			InReplyToID: replyToID,
			Visibility:  visibility,
			Language:    replyPost.Language,
			SpoilerText: contentWarning,
		})

		if err != nil {
			log.Printf("Error posting reply: %v", err)
		}
	}
}

// generateAltText generates alt-text for an image using Gemini AI or Ollama
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

	// Downscale the image to a smaller width using config settings
	downscaledImg, format, err := downscaleImage(img, config.ImageProcessing.DownscaleWidth)
	if err != nil {
		return "", err
	}

	LogEvent("alt_text_generated")

	prompt := getLocalizedString(lang, "generateAltText", "prompt")

	fmt.Println("Processing image: " + imageURL)

	switch config.LLM.Provider {
	case "gemini":
		return GenerateAltWithGemini(prompt, downscaledImg, format)
	case "ollama":
		return GenerateAltWithOllama(prompt, downscaledImg, format)
	default:
		return "", fmt.Errorf("unsupported LLM provider: %s", config.LLM.Provider)
	}
}

// Generate creates a response using the Gemini AI model
func GenerateAltWithGemini(strPrompt string, image []byte, fileExtension string) (string, error) {
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

// GenerateAltWithOllama generates alt-text using the Ollama model
func GenerateAltWithOllama(strPrompt string, image []byte, fileExtension string) (string, error) {
	// Save the image temporarily
	tmpFile, err := os.CreateTemp("", "image.*."+fileExtension)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(image); err != nil {
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	// Run the Ollama command
	return runOllamaCommand(strPrompt, tmpFile.Name(), config.LLM.OllamaModel)
}

// runOllamaCommand runs the Ollama command to generate alt-text for an image
func runOllamaCommand(prompt, imagePath, model string) (string, error) {
	cmd := exec.Command("ollama", "run", model, fmt.Sprintf("%s %s", prompt, imagePath))

	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		return "", err
	}

	return out.String(), nil
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

// checkOllamaModel checks if the Ollama model is available and working
func checkOllamaModel() error {
	cmd := exec.Command("ollama", "list")

	var out bytes.Buffer
	cmd.Stdout = &out

	err := cmd.Run()
	if err != nil {
		return err
	}

	if !strings.Contains(out.String(), config.LLM.OllamaModel) {
		return fmt.Errorf("ollama model not found: %s\nInstall it via:\nollama run %s", config.LLM.OllamaModel, config.LLM.OllamaModel)
	}

	return nil
}
