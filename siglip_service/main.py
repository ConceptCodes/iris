import argparse
import logging
from concurrent import futures
from io import BytesIO

import grpc
import torch
from grpc_health.v1 import health, health_pb2, health_pb2_grpc
from PIL import Image
from transformers import AutoModel, AutoProcessor

from clip.v1 import clip_pb2, clip_pb2_grpc

MAX_IMAGE_BYTES = 20 * 1024 * 1024
MAX_IMAGE_PIXELS = 100_000_000
MAX_GRPC_MESSAGE_BYTES = MAX_IMAGE_BYTES + 1024 * 1024
SERVICE_NAME = "clip.v1.ClipService"

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)


def get_device():
    if torch.cuda.is_available():
        return "cuda", f"cuda:{torch.cuda.get_device_name(0)}"
    if hasattr(torch.backends, "mps") and torch.backends.mps.is_available():
        return "mps", "mps (Apple Metal)"
    return "cpu", "cpu"


def normalize_features(features):
    return features / features.norm(dim=-1, keepdim=True)


class SigLIP2Service(clip_pb2_grpc.ClipServiceServicer):
    def __init__(self, model_name: str):
        self.device, self.device_name = get_device()
        logger.info("using device: %s", self.device_name)
        logger.info("loading SigLIP2 model: %s", model_name)
        self.processor = AutoProcessor.from_pretrained(model_name)
        self.model = AutoModel.from_pretrained(model_name).to(self.device)
        self.model.eval()
        logger.info("model loaded successfully")

    def EmbedText(self, request, context):
        with torch.no_grad():
            inputs = self.processor(text=[request.text], padding=True, return_tensors="pt")
            inputs = {key: value.to(self.device) for key, value in inputs.items()}
            text_features = normalize_features(self.model.get_text_features(**inputs))
            embedding = text_features[0].cpu().tolist()
        return clip_pb2.EmbedResponse(embedding=embedding, dim=len(embedding))

    def EmbedImage(self, request, context):
        image_bytes = request.image_bytes
        if len(image_bytes) > MAX_IMAGE_BYTES:
            context.abort(grpc.StatusCode.RESOURCE_EXHAUSTED, "image size exceeds 20 MB limit")

        try:
            image = Image.open(BytesIO(image_bytes))
            image.load()
        except Exception as exc:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, f"invalid image: {exc}")

        width, height = image.size
        pixel_count = width * height
        if pixel_count > MAX_IMAGE_PIXELS:
            context.abort(
                grpc.StatusCode.RESOURCE_EXHAUSTED,
                (
                    f"image dimensions ({width}x{height}={pixel_count:,} pixels) exceed maximum allowed "
                    f"({MAX_IMAGE_PIXELS:,} pixels)"
                ),
            )

        image = image.convert("RGB")

        with torch.no_grad():
            inputs = self.processor(images=image, return_tensors="pt")
            inputs = {key: value.to(self.device) for key, value in inputs.items()}
            image_features = normalize_features(self.model.get_image_features(**inputs))
            embedding = image_features[0].cpu().tolist()
        return clip_pb2.EmbedResponse(embedding=embedding, dim=len(embedding))


def serve(model_name: str, host: str, port: int):
    service = SigLIP2Service(model_name)
    server = grpc.server(
        futures.ThreadPoolExecutor(max_workers=4),
        options=[
            ("grpc.max_receive_message_length", MAX_GRPC_MESSAGE_BYTES),
            ("grpc.max_send_message_length", MAX_GRPC_MESSAGE_BYTES),
        ],
    )
    clip_pb2_grpc.add_ClipServiceServicer_to_server(service, server)

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
    parser.add_argument("--model", default="google/siglip2-base-patch16-224")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8002)
    args = parser.parse_args()
    serve(args.model, args.host, args.port)
