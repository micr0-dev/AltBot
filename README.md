<div align="center">
  <img src="assets/micr0-alty-banner.png" alt="A decorative banner featuring a repeating pattern of small purple robot icons against a light background, creating a retro-tech wallpaper effect">

  # Altbot アクセシビリティロボット
  
  *Making the Fediverse more inclusive, one image at a time*

  [![Latest Release](https://img.shields.io/github/v/release/micr0-dev/Altbot)](https://github.com/micr0-dev/Altbot/releases)
  [![Mastodon Follow](https://img.shields.io/mastodon/follow/113183205946060973?domain=fuzzies.wtf&style=social)](https://fuzzies.wtf/@altbot)
  [![License: OWL](https://img.shields.io/badge/license-OWL-purple.svg)](https://owl-license.org/)
  [![Go Version](https://img.shields.io/github/go-mod/go-version/micr0-dev/Altbot)](https://go.dev/)
  ![Status](https://img.shields.io/badge/status-active-success)
  ![Environment](https://img.shields.io/badge/environment-friendly-green)
</div>

## About

Altbot is an open-source accessibility bot designed to enhance the Fediverse by generating alt-text descriptions for images, video, and audio. This helps make content more accessible to users with visual impairments.

## How It Works

Altbot listens for mentions and follows on Mastodon. When it detects a mention or a new post from a followed user, it checks for images without alt-text. If it finds any, it uses a Large Language Model (LLM) to generate descriptive alt-text and replies with the generated text.

### Features

- **Mention-Based Alt-Text Generation:** Mention @Altbot in a reply to any post containing an image, video, or audio, and Altbot will generate an alt-text description for it.
- **Automatic Alt-Text for Followers:** Follow @Altbot, and it will monitor your posts. If you post an image, video, or audio without alt-text, Altbot will automatically generate one for you.
- **Local LLM Support:** Use local LLMs via Ollama for generating alt-text descriptions.
- **Consent Requests:** Ask for consent from the original poster before generating alt-text when mentioned by non-OP users.
- **Configurable Settings:** Easily configure the bot using a TOML file.

## Privacy Note

Your post content is never used. Only images without existing alt-text will be processed.

## Disclaimer

Alt-texts are generated using a Large Language Model (LLM). While we strive for accuracy, results may sometimes be factually incorrect. Always double-check the alt-text before using it.

## Setup

1. Clone the repository:
    ```sh
    git clone https://github.com/micr0-dev/Altbot.git
    cd Altbot
    ```

2. Copy the example configuration file and edit it:
    ```sh
    cp example.config.toml config.toml
    ```

    Open `config.toml` in your favorite text editor and update the values as needed. Here is an example of what the file looks like:

    ```toml
    [server]
    mastodon_server = "https://mastodon.example.com" # Your Mastodon server URL
    client_secret = "your_client_secret"             # Your Mastodon App client secret
    access_token = "your_access_token"               # Your Mastodon App access token
    username = "your_bot_username"                   # Your Mastodon bot's username

    [llm]
    provider = "gemini"         # or "ollama"
    ollama_model = "llava-phi3"

    [gemini]
    api_key = "your_gemini_api_key" # Replace with your Gemini API key, if you don't have one, you can get it from https://aistudio.google.com/app/apikey
    model = "gemini-1.5-flash"      # or "gemini-1.5-pro" Note: "gemini-1.5-pro" allows for only 2 Requests per Minute while "gemini-1.5-flash" allows for 15 Requests per Minute
    temperature = 0.7
    top_k = 1
    # Thresholds for the content moderation, setting them to another value than "none" will enable the content moderation may brake some responses
    # Can be set to "none", "low", "medium", "high"
    harassment_threshold = "none"
    hate_speech_threshold = "none"
    sexually_explicit_threshold = "none"
    dangerous_content_threshold = "none"

    [localization]
    # Default language for the bot
    default_language = "en"

    [dni]
    # List of profile tags that will make the bot ignore the user
    tags = ["#nobot", "#noai", "#nollm"]
    # Should the bot ignore other automated accounts
    ignore_bots = true

    [image_processing]
    # Greater values may break the image processing due to having a size greater than the maximum allowed by the API
    downscale_width = 800

    [behavior]
    # Maximum visibility of the replies to the bot, can be "public", "unlisted", "private" or "direct"
    reply_visibility = "unlisted"
    # Follow back new followers
    follow_back = true
    # Ask for consent when mentioned by none OP users
    ask_for_consent = true
    ```

3. Install dependencies:
    ```sh
    go mod tidy
    ```

4. Run the bot:
    ```sh
    go run main.go
    ```

## Contributing

We welcome contributions! Please open an issue or submit a pull request with your improvements.

## License

This project is licensed under the [OVERWORKED LICENSE (OWL) v1.0.](https://owl-license.org/) See the [LICENSE](LICENSE) file for details.

---

Join us in making the Fediverse a more inclusive place for everyone!
