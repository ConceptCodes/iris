import argparse
import logging
import os
import re
from concurrent import futures
from io import BytesIO

import easyocr
import grpc
import numpy as np
import torch
from grpc_health.v1 import health, health_pb2, health_pb2_grpc
from PIL import Image, UnidentifiedImageError
from transformers import BlipForConditionalGeneration, BlipProcessor

from metadata.v1 import metadata_pb2, metadata_pb2_grpc

MAX_IMAGE_BYTES = int(os.getenv("MAX_IMAGE_BYTES", 20 * 1024 * 1024))
MAX_IMAGE_PIXELS = int(os.getenv("MAX_IMAGE_PIXELS", 100_000_000))
MAX_GRPC_MESSAGE_BYTES = int(os.getenv("MAX_GRPC_MESSAGE_BYTES", MAX_IMAGE_BYTES + 1024 * 1024))
SERVICE_NAME = "metadata.v1.MetadataService"
STOPWORDS = {
    "a", "an", "and", "at", "background", "blue", "by", "for", "from", "in",
    "is", "it", "of", "on", "or", "photo", "picture", "shows", "small", "the",
    "to", "with",
}

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def get_device():
    if torch.cuda.is_available():
        return "cuda", f"cuda:{torch.cuda.get_device_name(0)}"
    if hasattr(torch.backends, "mps") and torch.backends.mps.is_available():
        return "mps", "mps (Apple Metal)"
    return "cpu", "cpu"


class MetadataService(metadata_pb2_grpc.MetadataServiceServicer):
    def __init__(self, caption_model: str, ocr_langs: list[str]):
        self.device, self.device_name = get_device()
        logger.info("using device: %s", self.device_name)
        logger.info("loading caption model: %s", caption_model)
        self.caption_processor = BlipProcessor.from_pretrained(caption_model)
        self.caption_model = BlipForConditionalGeneration.from_pretrained(caption_model).to(self.device)
        self.caption_model.eval()

        logger.info("loading OCR languages: %s", ocr_langs)
        self.ocr_reader = easyocr.Reader(ocr_langs, gpu=self.device == "cuda")

    def AnalyzeImage(self, request, context):
        image_bytes = request.image_bytes
        if len(image_bytes) > MAX_IMAGE_BYTES:
            context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, "image size exceeds 20 MB limit")

        try:
            image = Image.open(BytesIO(image_bytes))
            image.load()
        except UnidentifiedImageError as exc:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, f"invalid image: {exc}")

        width, height = image.size
        if width * height > MAX_IMAGE_PIXELS:
            context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, "image dimensions exceed maximum allowed pixels")

        image = image.convert("RGB")
        caption = self._caption(image)
        ocr_text = self._ocr(image)
        tags = self._tags(caption, ocr_text)
        return metadata_pb2.AnalyzeImageResponse(caption=caption, ocr_text=ocr_text, tags=tags)

    def _caption(self, image: Image.Image) -> str:
        with torch.no_grad():
            inputs = self.caption_processor(images=image, return_tensors="pt")
            inputs = {key: value.to(self.device) for key, value in inputs.items()}
            output = self.caption_model.generate(**inputs, max_new_tokens=32)
        return self.caption_processor.decode(output[0], skip_special_tokens=True).strip()

    def _ocr(self, image: Image.Image) -> str:
        results = self.ocr_reader.readtext(np.array(image), detail=0, paragraph=True)
        return " ".join(part.strip() for part in results if part.strip())

    def _tags(self, caption: str, ocr_text: str):
        tags = []
        seen = set()

        def add(tag: str):
            tag = re.sub(r"[^a-z0-9]+", "-", tag.lower()).strip("-")
            if len(tag) < 3 or tag in STOPWORDS or tag in seen:
                return
            seen.add(tag)
            tags.append(tag)

        for token in re.findall(r"[A-Za-z0-9']+", caption.lower()):
            add(token)
        if ocr_text.strip():
            add("text")
            words = re.findall(r"[A-Za-z0-9']+", ocr_text.lower())
            if len(words) >= 5:
                add("document")
            for token in words[:6]:
                add(token)
        return tags[:12]


def serve(caption_model: str, ocr_langs: list[str], host: str, port: int):
    service = MetadataService(caption_model, ocr_langs)
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=4),
        options=[
            ("grpc.max_receive_message_length", MAX_GRPC_MESSAGE_BYTES),
            ("grpc.max_send_message_length", MAX_GRPC_MESSAGE_BYTES),
        ],
    )
    metadata_pb2_grpc.add_MetadataServiceServicer_to_server(service, server)

    health_servicer = health.HealthServicer()
    health_pb2_grpc.add_HealthServicer_to_server(health_servicer, server)
    health_servicer.set("", health_pb2.HealthCheckResponse.SERVING)
    health_servicer.set(SERVICE_NAME, health_pb2.HealthCheckResponse.SERVING)

    listen_addr = f"{host}:{port}"
    server.add_insecure_port(listen_addr)
    logger.info("starting gRPC server on %s", listen_addr)
    server.start()
    server.wait_for_termination()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--host", default=os.getenv("METADATA_HOST", "0.0.0.0"))
    parser.add_argument("--port", type=int, default=int(os.getenv("METADATA_PORT", "8003")))
    parser.add_argument("--caption-model", default=os.getenv("METADATA_CAPTION_MODEL", "Salesforce/blip-image-captioning-base"))
    parser.add_argument("--ocr-langs", default=os.getenv("METADATA_OCR_LANGS", "en"))
    args = parser.parse_args()
    serve(args.caption_model, [lang.strip() for lang in args.ocr_langs.split(",") if lang.strip()], args.host, args.port)
