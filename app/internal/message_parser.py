from asyncio import Semaphore
from datetime import datetime

import langchain
from langchain.output_parsers import OutputFixingParser
from langchain_core.exceptions import OutputParserException
from langchain_core.prompts import PromptTemplate
from langchain_core.runnables import Runnable
from langchain_ollama import OllamaLLM

from .prompt import BaseParser, ResponseSchema, template

model = "llama3.1:8b"
ollama_url = "http://localhost:11434"
max_concurrent_prompts = 5


class MessageParser:
    base_chain: Runnable
    retry_chain: Runnable
    fixing_parser: OutputFixingParser
    deub: bool
    semaphore: Semaphore

    def __init__(self, debug=False):
        self.debug = debug
        self.semaphore = Semaphore(max_concurrent_prompts)
        langchain.debug = self.debug

        base_parser = BaseParser()
        llm = OllamaLLM(model=model, base_url=ollama_url, temperature=0.1, top_p=0.15, top_k=10)
        llm_retry = OllamaLLM(
            model=model, base_url=ollama_url, temperature=0.5, top_p=0.5, top_k=25
        )

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
        return max_concurrent_prompts - self.semaphore._value

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
