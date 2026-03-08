import argparse
import base64
import logging
from contextlib import asynccontextmanager
from io import BytesIO

import torch
import uvicorn
from fastapi import FastAPI, HTTPException
from open_clip import create_model_from_pretrained, get_tokenizer
from PIL import Image
from pydantic import BaseModel

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

model = None
tokenizer = None
preprocess = None
device = None
device_name = None


def get_device():
    if torch.cuda.is_available():
        return "cuda", f"cuda:{torch.cuda.get_device_name(0)}"
    if hasattr(torch.backends, "mps") and torch.backends.mps.is_available():
        return "mps", "mps (Apple Metal)"
    return "cpu", "cpu"


class TextRequest(BaseModel):
    text: str


class ImageRequest(BaseModel):
    image_b64: str


class EmbedResponse(BaseModel):
    embedding: list[float]
    dim: int


class HealthResponse(BaseModel):
    status: str
    device: str


@asynccontextmanager
async def lifespan(app: FastAPI):
    global model, tokenizer, preprocess, device, device_name
    device, device_name = get_device()
    logger.info(f"Using device: {device_name}")
    model_name = app.state.model_name
    logger.info(f"Loading model: {model_name}")
    model, preprocess = create_model_from_pretrained(f"hf-hub:laion/{model_name}")
    model = model.to(device)
    model.eval()
    tokenizer = get_tokenizer(f"hf-hub:laion/{model_name}")
    logger.info("Model loaded successfully")
    yield


app = FastAPI(lifespan=lifespan)


@app.get("/health", response_model=HealthResponse)
def health():
    return HealthResponse(status="ok", device=device_name)


@app.post("/embed/text", response_model=EmbedResponse)
def embed_text(req: TextRequest):
    with torch.no_grad():
        text_tokens = tokenizer([req.text])
        text_features = model.encode_text(text_tokens.to(device))
        text_features = text_features / text_features.norm(dim=-1, keepdim=True)
        embedding = text_features[0].cpu().tolist()
    return EmbedResponse(embedding=embedding, dim=len(embedding))


@app.post("/embed/image", response_model=EmbedResponse)
def embed_image(req: ImageRequest):
    try:
        image_bytes = base64.b64decode(req.image_b64)
        if len(image_bytes) > 20 * 1024 * 1024:  # 20 MB limit
            raise HTTPException(status_code=413, detail="Image size exceeds 20 MB limit")
        image = Image.open(BytesIO(image_bytes)).convert("RGB")
    except Exception as e:
        raise HTTPException(status_code=400, detail=f"invalid image: {e}")

    with torch.no_grad():
        image_tensor = preprocess(image).unsqueeze(0).to(device)
        image_features = model.encode_image(image_tensor)
        image_features = image_features / image_features.norm(dim=-1, keepdim=True)
        embedding = image_features[0].cpu().tolist()
    return EmbedResponse(embedding=embedding, dim=len(embedding))


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", default="CLIP-ViT-B-32-laion2B-s34B-b79K")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8001)
    args = parser.parse_args()
    app.state.model_name = args.model
    uvicorn.run(app, host=args.host, port=args.port)
