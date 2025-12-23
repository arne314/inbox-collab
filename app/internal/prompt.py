import re
from datetime import datetime
from typing import List, Optional

from dateutil import parser as dateutil_parser
from pydantic import BaseModel, Field, field_validator, model_validator
from typing_extensions import Self

from .strings import (
    template_forward,
    template_forward_format1,
    template_forward_format2,
    template_multiple,
    template_task_multiple,
    template_task_single,
)

placeholder_regex = re.compile(r"==\s*PLACEHOLDER\s*==")


class MessageSchema(BaseModel):
    author: str = Field(..., description="Message author")
    content: str = Field(default="", description="Message content")
    timestamp: Optional[str] = Field(default=None, description="Message timestamp")

    @field_validator("timestamp", mode="before")
    @classmethod
    def parse_timestamp(cls, value) -> str | None:
        if isinstance(value, str):
            return dateutil_parser.parse(value, fuzzy=True).isoformat()

    def is_placeholder(self) -> bool:
        if placeholder_regex.search(self.content):
            return True
        return False


class ResponseSchema(BaseModel):
    messages: List[MessageSchema] = Field(
        ...,
        description="Ordered list message objects containing the author of the message and then the message itself",
    )
    forwarded: bool = Field(default=False, description="Whether the conversation was forwarded")
    forwarded_by: str | None = Field(default=None, description="Person who forwarded the mail")

    @field_validator("messages")
    @classmethod
    def validate_messages(cls, messages):
        if not messages:
            raise ValueError("Extract at least one message")

        if all(msg.timestamp is None for msg in messages):
            raise ValueError(
                "Set the `timestamp` of the most recent message to the one given in the prompt"
            )

        # put most recent message first
        messages.sort(
            key=lambda m: datetime.fromisoformat(m.timestamp)
            if m.timestamp is not None
            else datetime.fromtimestamp(0),
            reverse=True,
        )
        # make sure the first message is not a placeholder
        if messages[0].is_placeholder():
            for i, message in enumerate(messages):
                if not message.is_placeholder():
                    messages[0], messages[i] = messages[i], messages[0]
                    break
            else:
                raise ValueError(
                    "There is at least one message without a placeholder. Please include it properly."
                )
        return messages

    @model_validator(mode="after")
    def validate_forwarded(self) -> Self:
        if (self.forwarded and (self.forwarded_by is None or self.forwarded_by.isspace())) or (
            not self.forwarded and self.forwarded_by is not None and not self.forwarded_by.isspace()
        ):
            raise ValueError(
                "Set the `forwarded` boolean and `forwarded_by` string according to the given conversation"
            )
        if not self.forwarded:
            self.forwarded_by = None
        return self


def generate_prompt_inputs(
    conversation: str,
    author: str,
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
        "author": author,
        "subject": subject,
        "timestamp": timestamp.strftime("%Y-%m-%dT%H:%M"),
        "task": template_task_multiple if multiple else template_task_single,
        "template_multiple": optional(multiple, template_multiple),
        "template_forward": optional(forward_candidate, template_forward),
        "forward_format1": optional(forward_candidate, template_forward_format1),
        "forward_format2": optional(forward_candidate, template_forward_format2),
    }
    return inputs
