import sys

from fastapi import FastAPI
from pydantic import BaseModel

from internal import MessageParser

app = FastAPI()
message_parser = MessageParser("dev" in sys.argv)


@app.get("/")
async def read_root():
    return {"concurrent_prompts": message_parser.get_concurrent_prompts()}


class ParseMessagesRequest(BaseModel):
    conversation: str


@app.post("/parse_messages")
async def parse_messages(req: ParseMessagesRequest):
    return await message_parser.parse_messages(req.conversation)
