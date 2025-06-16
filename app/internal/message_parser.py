from asyncio import Semaphore
from datetime import datetime

import langchain
from langchain.output_parsers import OutputFixingParser
from langchain_core.exceptions import OutputParserException
from langchain_core.prompts import PromptTemplate
from langchain_core.rate_limiters import InMemoryRateLimiter
from langchain_core.runnables import Runnable
from langchain_ollama import OllamaLLM
from langchain_openai import ChatOpenAI

from .prompt import BaseParser, MessageSchema, ResponseSchema, generate_prompt_inputs
from .strings import template


class MessageParser:
    chain: Runnable
    base_parser: BaseParser
    fixing_parser: OutputFixingParser
    debug: bool
    semaphore: Semaphore
    max_concurrent_prompts: int

    def __init__(self, debug, config):
        self.debug = debug
        self.max_concurrent_prompts = config.get("max_concurrent_prompts", 5)
        self.semaphore = Semaphore(self.max_concurrent_prompts)
        langchain.debug = self.debug  # pyright: ignore

        self.base_parser = BaseParser()
        llm = OllamaLLM(
            model="llama3.1:8b",
            base_url="http://localhost:11434",
            temperature=0.1,
            top_p=0.15,
            top_k=10,
        )
        llm_retry = None

        if (llm_config_model := config.get("openai_model")) is not None:
            assert llm.temperature is not None
            request_limiter = None
            if (rate_limit := config.get("rate_limit")) is not None:
                request_limiter = InMemoryRateLimiter(
                    requests_per_second=rate_limit,
                    max_bucket_size=config.get("rate_limit_max_bucket", 1),
                )
            llm = ChatOpenAI(
                model=llm_config_model,
                base_url=config.get("openai_url"),
                api_key=config.get("openai_api_key"),
                temperature=llm.temperature,
                rate_limiter=request_limiter,
                max_retries=config.get("openai_max_retries", 10),
            )
        elif (llm_config_model := config.get("ollama_model")) is not None:
            llm.model = llm_config_model
            if (llm_config_url := config.get("ollama_url")) is not None:
                llm.base_url = llm_config_url

        if isinstance(llm, OllamaLLM):
            llm_retry = OllamaLLM(
                model=llm.model, base_url=llm.base_url, temperature=0.5, top_p=0.5, top_k=25
            )
        elif isinstance(llm, ChatOpenAI):
            llm_retry = ChatOpenAI(
                model=llm.model_name,
                base_url=llm.openai_api_base,
                api_key=llm.openai_api_key,
                rate_limiter=llm.rate_limiter,
                max_retries=llm.max_retries,
                temperature=0.5,
            )
        assert llm_retry is not None, "Retry llm could not be set, check you config"
        print(f"Setup llm provider: {llm}")

        prompt = PromptTemplate(
            template=template,
            input_variables=[
                "conversation",
                "subject",
                "timestamp",
                "task",
                "template_multiple",
                "template_forward",
                "format_instructions",
                "forward_format1",
                "forward_format2",
            ],
        )
        self.chain = prompt | llm
        self.fixing_parser = OutputFixingParser.from_llm(
            parser=self.base_parser, llm=llm_retry, max_retries=2
        )

    def get_concurrent_prompts(self) -> int:
        return self.max_concurrent_prompts - self.semaphore._value

    async def parse_messages(
        self,
        conversation: str,
        subject: str,
        timestamp: datetime,
        reply_candidate: bool,
        forward_candidate: bool,
    ) -> ResponseSchema:
        async with self.semaphore:
            inputs = generate_prompt_inputs(
                conversation,
                subject,
                timestamp,
                reply_candidate,
                forward_candidate,
            )
            output = await self.chain.ainvoke(inputs)

            try:
                parsed: ResponseSchema = await self.fixing_parser.ainvoke(output)
                if len(parsed.messages) == 1:
                    parsed.messages[0].timestamp = timestamp

                print("Message extraction successful")
                if self.debug:
                    for i, msg in enumerate(parsed.messages):
                        print(
                            f"Message {'(forwarded) ' if parsed.forwarded else ''}{i + 1} from {msg.author} at {msg.timestamp}:\n{msg.content}\n"
                        )
                return parsed
            except OutputParserException:
                print("Failed to extract messages")
                return ResponseSchema(
                    messages=[
                        MessageSchema(
                            author="Error extracting messages",
                            content=conversation,
                            timestamp=timestamp,
                        ),
                    ],
                )
