import os
import sys
import tomllib
from datetime import datetime

from dotenv import load_dotenv
from fastapi import FastAPI
from pydantic import BaseModel

from internal import MessageParser

load_dotenv()
with open("config/config.toml", "rb") as f:
    config = tomllib.load(f)
    config["llm"]["openai_api_key"] = os.getenv("OPENAI_API_KEY")

app = FastAPI()
message_parser = MessageParser("dev" in sys.argv, config.get("llm", {}))


@app.get("/")
async def read_root():
    return {"concurrent_prompts": message_parser.get_concurrent_prompts()}


class ParseMessagesRequest(BaseModel):
    conversation: str
    subject: str
    timestamp: datetime
    reply_candidate: bool
    forward_candidate: bool


@app.post("/parse_messages")
async def parse_messages(req: ParseMessagesRequest):
    return await message_parser.parse_messages(
        req.conversation, req.subject, req.timestamp, req.reply_candidate, req.forward_candidate
    )
