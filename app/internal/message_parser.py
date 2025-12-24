from asyncio import Semaphore
from datetime import datetime

import langchain
from langchain.agents import create_agent
from langchain.agents.middleware import ModelCallLimitMiddleware
from langchain.agents.structured_output import ToolStrategy
from langchain_core.prompts import ChatPromptTemplate, PromptTemplate
from langchain_core.rate_limiters import InMemoryRateLimiter
from langchain_ollama import ChatOllama
from langchain_openai import ChatOpenAI

from .prompt import MessageSchema, ResponseSchema, generate_prompt_inputs
from .strings import template_format_instructions, template_post, template_pre


class MessageParser:
    prompt: PromptTemplate
    debug: bool
    semaphore: Semaphore
    max_concurrent_prompts: int

    def __init__(self, debug, config):
        self.debug = debug
        self.max_concurrent_prompts = config.get("max_concurrent_prompts", 5)
        self.semaphore = Semaphore(self.max_concurrent_prompts)
        langchain.debug = self.debug  # pyright: ignore
        if debug:
            from openinference.instrumentation.langchain import LangChainInstrumentor
            from phoenix.otel import register as register_tracer

            tracer_provider = register_tracer(
                project_name="inbox-collab-dev",
                endpoint="http://localhost:6006/v1/traces",
            )
            LangChainInstrumentor().instrument(tracer_provider=tracer_provider)

        llm = ChatOllama(
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

        if isinstance(llm, ChatOllama):
            llm_retry = ChatOllama(
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

        def setup_agent(model):
            return create_agent(
                model=model,
                tools=[],
                response_format=ToolStrategy(ResponseSchema),
                middleware=[ModelCallLimitMiddleware(run_limit=4, exit_behavior="error")],
            )

        prompt = ChatPromptTemplate.from_messages(
            [
                ("system", template_pre + template_format_instructions),
                ("human", template_post),
            ]
        )
        self.chain = prompt | setup_agent(llm)
        self.chain_retry = prompt | setup_agent(llm_retry)

    def get_concurrent_prompts(self) -> int:
        return self.max_concurrent_prompts - self.semaphore._value

    async def parse_messages(
        self,
        conversation: str,
        author: str,
        subject: str,
        timestamp: datetime,
        reply_candidate: bool,
        forward_candidate: bool,
    ) -> ResponseSchema:
        async with self.semaphore:
            inputs = generate_prompt_inputs(
                conversation,
                author,
                subject,
                timestamp,
                reply_candidate,
                forward_candidate,
            )

            for chain in [self.chain, self.chain_retry]:
                try:
                    result = await chain.ainvoke(inputs)
                    parsed: ResponseSchema = result["structured_response"]
                    parsed.messages[0].timestamp = timestamp.astimezone().isoformat()
                    print("Message extraction successful")
                    if self.debug:
                        for i, msg in enumerate(parsed.messages):
                            print(
                                f"Message {'(forwarded) ' if parsed.forwarded else ''}{i + 1} from {msg.author} at {msg.timestamp}:\n{msg.content}\n"
                            )
                    return parsed
                except Exception:
                    print("Failed to extract messages, retrying with different model...")
                    continue

            print("Failed to extract messages, returning unmodified input")
            # validation must be skipped as there might be placeholder block
            return ResponseSchema.model_construct(
                messages=[
                    MessageSchema(
                        author="Error extracting messages",
                        content=conversation,
                        timestamp=timestamp.astimezone().isoformat(),
                    ),
                ],
            )
