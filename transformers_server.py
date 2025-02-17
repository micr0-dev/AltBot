from flask import Flask, request, jsonify  # type: ignore
import logging
import argparse
from transformers import AutoModelForCausalLM
import torch
from PIL import Image
import base64
import io

# Configure logging
logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

app = Flask(__name__)

# Global variables
model = None
text_tokenizer = None
visual_tokenizer = None


def parse_args():
    parser = argparse.ArgumentParser(description="Transformers Server")
    parser.add_argument("--port", type=int, default=8000)
    parser.add_argument("--model", type=str, required=True)
    parser.add_argument("--device", type=str, default="cuda")
    parser.add_argument("--max-memory", type=float, default=0.9)
    parser.add_argument("--torch-dtype", type=str, default="bfloat16")
    return parser.parse_args()


def load_model(args):
    global model, text_tokenizer, visual_tokenizer
    logger.info(f"Loading model {args.model}...")

    dtype_map = {
        "float32": torch.float32,
        "float16": torch.float16,
        "bfloat16": torch.bfloat16,
    }

    model = AutoModelForCausalLM.from_pretrained(
        args.model,
        torch_dtype=dtype_map[args.torch_dtype],
        multimodal_max_length=32768,
        trust_remote_code=True,
        device_map=args.device,
    )

    text_tokenizer = model.get_text_tokenizer()
    visual_tokenizer = model.get_visual_tokenizer()
    logger.info("Model loaded successfully!")


@app.route("/health", methods=["GET"])
def health():
    return jsonify({"status": "healthy"}), 200


@app.route("/v1/chat/completions", methods=["POST"])
def chat_completions():
    logger.info("Received request to /v1/chat/completions")
    try:
        data = request.json
        messages = data.get("messages", [])

        if not messages:
            return jsonify({"error": "No messages provided"}), 400

        content = messages[0].get("content", [])
        prompt = None
        image_data = None

        for item in content:
            if item["type"] == "text":
                prompt = item["text"]
            elif item["type"] == "image_url":
                image_url = item["image_url"]["url"]
                image_data = base64.b64decode(image_url.split(",")[1])

        if not prompt or not image_data:
            return jsonify({"error": "Missing prompt or image"}), 400

        image = Image.open(io.BytesIO(image_data))
        images = [image]

        query = f"<image>\n{prompt}"
        prompt, input_ids, pixel_values = model.preprocess_inputs(
            query, images, max_partition=9
        )

        attention_mask = torch.ne(input_ids, text_tokenizer.pad_token_id)
        input_ids = input_ids.unsqueeze(0).to(device=model.device)
        attention_mask = attention_mask.unsqueeze(0).to(device=model.device)

        if pixel_values is not None:
            pixel_values = pixel_values.to(
                dtype=visual_tokenizer.dtype, device=visual_tokenizer.device
            )
        pixel_values = [pixel_values]

        with torch.inference_mode():
            gen_kwargs = dict(
                max_new_tokens=1024,
                do_sample=False,
                top_p=None,
                top_k=None,
                temperature=None,
                repetition_penalty=None,
                eos_token_id=model.generation_config.eos_token_id,
                pad_token_id=text_tokenizer.pad_token_id,
                use_cache=True,
            )
            output_ids = model.generate(
                input_ids,
                pixel_values=pixel_values,
                attention_mask=attention_mask,
                **gen_kwargs,
            )[0]
            output = text_tokenizer.decode(output_ids, skip_special_tokens=True)

        return jsonify({"choices": [{"message": {"content": output}}]})

    except Exception as e:
        logger.error(f"Error processing request: {str(e)}")
        return jsonify({"error": str(e)}), 500


if __name__ == "__main__":
    args = parse_args()
    load_model(args)
    logger.info(f"Starting server on port {args.port}")
    app.run(host="0.0.0.0", port=args.port)
