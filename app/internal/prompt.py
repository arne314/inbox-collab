from datetime import datetime
from typing import List

from langchain.output_parsers import PydanticOutputParser
from langchain_core.exceptions import OutputParserException
from pydantic import BaseModel, Field, field_serializer

from .strings import (
    template_forward,
    template_forward_format1,
    template_forward_format2,
    template_multiple,
    template_task_multiple,
    template_task_single,
)


class MessageSchema(BaseModel):
    author: str = Field(..., description="Message author")
    content: str = Field(default="", description="Message content")
    timestamp: datetime | None = Field(default=None, description="Message timestamp")

    @field_serializer("timestamp")
    def serialize_timestamp(self, value: datetime) -> str:
        return value.astimezone().isoformat()


class ResponseSchema(BaseModel):
    messages: List[MessageSchema] = Field(
        ...,
        description="Ordered list of Tuples containing the author of the message and then the message itself",
    )
    forwarded: bool = Field(default=False, description="Whether the conversation was forwarded")
    forwarded_by: str | None = Field(default=None, description="Person who forwarded the mail")


class BaseParser(PydanticOutputParser):
    def __init__(self):
        super().__init__(pydantic_object=ResponseSchema)

    def parse(self, text):  # additional data validation and processing
        parsed: ResponseSchema = super().parse(text)
        if all(msg.timestamp is None for msg in parsed.messages):
            raise OutputParserException(
                "Please set the `timestamp` of the most recent message to the one given in the prompt"
            )
        if (
            parsed.forwarded and (parsed.forwarded_by is None or parsed.forwarded_by.isspace())
        ) or (
            not parsed.forwarded
            and parsed.forwarded_by is not None
            and not parsed.forwarded_by.isspace()
        ):
            raise OutputParserException(
                "Please set the `forwarded` boolean and `forwarded_by` string according to the given conversation"
            )
        parsed.messages.sort(
            key=lambda m: m.timestamp if m.timestamp is not None else datetime.fromtimestamp(0),
            reverse=True,  # most recent message first
        )
        if not parsed.forwarded:
            parsed.forwarded_by = None
        return parsed


def generate_prompt_inputs(
    conversation: str,
    subject: str,
    timestamp: datetime,
    reply_candidate: bool,
    forward_candidate: bool,
):
    def optional(condition: bool, template) -> str:
        if condition:
            return template
        return ""

    multiple = reply_candidate or forward_candidate
    inputs = {
        "conversation": conversation,
        "subject": subject,
        "timestamp": timestamp.strftime("%Y-%m-%dT%H:%M"),
        "task": template_task_multiple if multiple else template_task_single,
        "template_multiple": optional(multiple, template_multiple),
        "template_forward": optional(forward_candidate, template_forward),
        "forward_format1": optional(forward_candidate, template_forward_format1),
        "forward_format2": optional(forward_candidate, template_forward_format2),
    }
    return inputs
