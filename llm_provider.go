// llm_provider.go
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// LLMProvider interface defines the methods that all LLM providers must implement
type LLMProvider interface {
	GenerateAltText(prompt string, imageData []byte, format string) (string, error)
	Close() error
}

// GeminiProvider implements LLMProvider for Google's Gemini
type GeminiProvider struct {
	model  *genai.GenerativeModel
	client *genai.Client
}

// OllamaProvider implements LLMProvider for Ollama
type OllamaProvider struct {
	model string
}

// VLLMProvider implements LLMProvider for vLLM
type VLLMProvider struct {
	ServerURL string
	Model     string
}

// NewLLMProvider creates a new LLM provider based on the configuration
func NewLLMProvider(config Config) (LLMProvider, error) {
	switch config.LLM.Provider {
	case "gemini":
		return setupGeminiProvider(config)
	case "ollama":
		return setupOllamaProvider(config)
	case "vllm":
		return setupVLLMProvider(config)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.LLM.Provider)
	}
}

// Setup functions for each provider
func setupGeminiProvider(config Config) (*GeminiProvider, error) {
	client, err := genai.NewClient(ctx, option.WithAPIKey(config.Gemini.APIKey))
	if err != nil {
		return nil, err
	}

	model := client.GenerativeModel(config.Gemini.Model)
	model.SetTemperature(config.Gemini.Temperature)
	model.SetTopK(config.Gemini.TopK)

	model.SafetySettings = []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: mapHarmBlock(config.Gemini.HarassmentThreshold),
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: mapHarmBlock(config.Gemini.HateSpeechThreshold),
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: mapHarmBlock(config.Gemini.SexuallyExplicitThreshold),
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: mapHarmBlock(config.Gemini.DangerousContentThreshold),
		},
	}

	return &GeminiProvider{
		model:  model,
		client: client,
	}, nil
}

func setupOllamaProvider(config Config) (*OllamaProvider, error) {
	// Check if Ollama is installed and the model is available
	cmd := exec.Command("ollama", "list")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("error checking Ollama installation: %v", err)
	}

	if !bytes.Contains(output, []byte(config.LLM.OllamaModel)) {
		return nil, fmt.Errorf("ollama model %s not found. Install it with: ollama pull %s",
			config.LLM.OllamaModel, config.LLM.OllamaModel)
	}

	return &OllamaProvider{
		model: config.LLM.OllamaModel,
	}, nil
}

func setupVLLMProvider(config Config) (*VLLMProvider, error) {
	return &VLLMProvider{
		ServerURL: config.LLM.VLLMServer,
		Model:     config.LLM.VLLMModel,
	}, nil
}

// GenerateAltText implementations for each provider
func (p *GeminiProvider) GenerateAltText(prompt string, imageData []byte, format string) (string, error) {
	var parts []genai.Part
	parts = append(parts, genai.Text(prompt))
	parts = append(parts, genai.ImageData(format, imageData))

	resp, err := p.model.GenerateContent(ctx, parts...)
	if err != nil {
		return "", err
	}

	return getResponse(resp), nil
}

func (p *OllamaProvider) GenerateAltText(prompt string, imageData []byte, format string) (string, error) {
	// Create a temporary file for the image
	tmpFile, err := os.CreateTemp("", "image.*"+format)
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(imageData); err != nil {
		return "", err
	}
	if err := tmpFile.Close(); err != nil {
		return "", err
	}

	// Prepare the Ollama command
	cmd := exec.Command("ollama", "run", p.model, fmt.Sprintf("%s %s", prompt, tmpFile.Name()))

	var out bytes.Buffer
	cmd.Stdout = &out

	err = cmd.Run()
	if err != nil {
		return "", err
	}

	return out.String(), nil
}

func (p *VLLMProvider) GenerateAltText(prompt string, imageData []byte, format string) (string, error) {
	// Convert image to base64
	base64Image := base64.StdEncoding.EncodeToString(imageData)

	// Prepare the request payload
	payload := map[string]interface{}{
		"model": p.Model,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": prompt,
					},
					{
						"type": "image_url",
						"image_url": map[string]interface{}{
							"url": fmt.Sprintf("data:image/%s;base64,%s", format, base64Image),
						},
					},
				},
			},
		},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	// Make the HTTP request to the vLLM server
	resp, err := http.Post(
		p.ServerURL+"/v1/chat/completions",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from vLLM server")
	}

	return result.Choices[0].Message.Content, nil
}

// Close implementations for each provider
func (p *GeminiProvider) Close() error {
	if p.client != nil {
		p.client.Close()
	}
	return nil
}

func (p *OllamaProvider) Close() error {
	return nil // Nothing to close for Ollama
}

func (p *VLLMProvider) Close() error {
	return nil // Server is managed separately
}

// Helper function to check vLLM server status
func checkVLLMServer(serverURL string) bool {
	resp, err := http.Get(serverURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// Helper function to start vLLM server
func startVLLMServer(model string) error {
	// Check if Python and vLLM are installed
	cmd := exec.Command("python3", "-c", "import vllm")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("vLLM not installed. Please install Python and run: pip install vllm")
	}

	// Start the vLLM server
	cmd = exec.Command("python3", "-m", "vllm.entrypoints.api_server",
		"--model", model,
		"--host", "localhost",
		"--port", "8000")

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start vLLM server: %v", err)
	}

	// Wait for server to start
	for i := 0; i < 30; i++ {
		if checkVLLMServer("http://localhost:8000") {
			return nil
		}
		time.Sleep(time.Second)
	}

	return fmt.Errorf("timeout waiting for vLLM server to start")
}
