from asyncio import Semaphore
from datetime import datetime

import langchain
from langchain.output_parsers import OutputFixingParser
from langchain_core.exceptions import OutputParserException
from langchain_core.prompts import PromptTemplate
from langchain_core.runnables import Runnable
from langchain_ollama import OllamaLLM
from langchain_openai import ChatOpenAI

from .prompt import BaseParser, ResponseSchema, template


class MessageParser:
    base_chain: Runnable
    retry_chain: Runnable
    fixing_parser: OutputFixingParser
    deub: bool
    semaphore: Semaphore
    max_concurrent_prompts: int

    def __init__(self, debug, config):
        self.debug = debug
        self.max_concurrent_prompts = config.get("max_concurrent_prompts", 5)
        self.semaphore = Semaphore(self.max_concurrent_prompts)
        langchain.debug = self.debug

        base_parser = BaseParser()
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
            llm = ChatOpenAI(
                model=llm_config_model,
                base_url=config.get("openai_url"),
                api_key=config.get("openai_api_key"),
                temperature=llm.temperature,
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
                temperature=0.5,
            )
        assert llm_retry is not None, "Retry llm could not be set, check you config"
        print(f"Setup llm provider: {llm}")

        prompt = PromptTemplate(
            template=template,
            input_variables=["conversation", "timestamp", "format_instructions"],
            partial_variables={
                "format_instructions": base_parser.get_format_instructions(),
            },
        )
        self.chain = prompt | llm
        self.fixing_parser = OutputFixingParser.from_llm(
            parser=base_parser, llm=llm_retry, max_retries=2
        )

    def get_concurrent_prompts(self) -> int:
        return self.max_concurrent_prompts - self.semaphore._value

    async def parse_messages(self, conversation: str, timestamp: datetime):
        async with self.semaphore:
            inputs = {"conversation": conversation, "timestamp": timestamp}
            output = await self.chain.ainvoke(inputs)

            try:
                parsed: ResponseSchema = await self.fixing_parser.ainvoke(output)
                print("Message extraction successful")
                if self.debug:
                    for i, msg in enumerate(parsed.messages):
                        print(
                            f"Message {"(forwarded) " if parsed.forwarded else ""}{i+1} from {msg.author} at {msg.timestamp}:\n{msg.content}\n"
                        )
                return parsed
            except OutputParserException:
                print("Failed to extract messages")
                return [["Error extracting messages", conversation]]
