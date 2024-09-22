# AltBot

AltBot is an open-source accessibility bot designed to enhance the Fediverse by generating alt-text descriptions for images. This helps make content more accessible to users with visual impairments.

## How It Works

AltBot listens for mentions and follows on Mastodon. When it detects a mention or a new post from a followed user, it checks for images without alt-text. If it finds any, it uses a Large Language Model (LLM) to generate descriptive alt-text and replies with the generated text.

### Features

- **Mention-Based Alt-Text Generation:** Mention @AltBot in a reply to any post containing an image, and AltBot will generate an alt-text description for it.
- **Automatic Alt-Text for Followers:** Follow @AltBot, and it will monitor your posts. If you post an image without alt-text, AltBot will automatically generate one for you.

## Privacy Note

Your post content is never used. Only images without existing alt-text will be processed.

## Disclaimer

Alt-texts are generated using a Large Language Model (LLM). While we strive for accuracy, results may sometimes be factually incorrect. Always double-check the alt-text before using it.

## Setup

1. Clone the repository:
    ```sh
    git clone https://github.com/micr0-dev/AltBot.git
    cd AltBot
    ```

2. Create a `.env` file with the following environment variables:
    ```env
    MASTODON_SERVER=https://mastodon.example.com
    MASTODON_CLIENT_ID=your_client_id
    MASTODON_CLIENT_SECRET=your_client_secret
    MASTODON_ACCESS_TOKEN=your_access_token
    MASTODON_USERNAME=your_bot_username
    GEMINI_API_KEY=your_gemini_api_key
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
