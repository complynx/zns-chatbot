import asyncio
import importlib
import json
import unittest
from types import SimpleNamespace
from unittest.mock import AsyncMock, Mock, patch

import httpx
from langchain_core.messages import HumanMessage, SystemMessage
from langchain_openai import ChatOpenAI


assistant_module = importlib.import_module("zns-chatbot.plugins.assistant")
config_module = importlib.import_module("zns-chatbot.config")


class AssistantOpenAITests(unittest.IsolatedAsyncioTestCase):
    async def test_one_shot_request_uses_openai_key_and_reply_limit(self):
        requests = []

        def respond(request):
            requests.append(request)
            return httpx.Response(200, json={
                "id": "chatcmpl-test",
                "object": "chat.completion",
                "created": 1,
                "model": "gpt-6-luna",
                "choices": [{
                    "index": 0,
                    "message": {"role": "assistant", "content": "Hello!"},
                    "finish_reason": "stop",
                }],
                "usage": {"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12},
            })

        config = SimpleNamespace(
            language_model=config_module.LanguageModel(
                openai_api_key="test-openai-key",
                google_api_key="test-google-key",
                reply_token_cap=123,
            ),
            mongo_db=SimpleNamespace(messages_collection="messages"),
        )
        app = SimpleNamespace(config=config, mongodb={"messages": Mock()}, users_collection=Mock())
        async with httpx.AsyncClient(transport=httpx.MockTransport(respond)) as client:
            with patch.object(
                assistant_module, "ChatOpenAI",
                side_effect=lambda **kwargs: ChatOpenAI(http_async_client=client, **kwargs),
            ), patch.object(assistant_module.Assistant, "_refresh_about", new_callable=AsyncMock):
                assistant = assistant_module.Assistant(app)
                await asyncio.sleep(0)
            reply = await assistant.chat.ainvoke([
                SystemMessage(content="You are Zinusya."),
                HumanMessage(content="Hello"),
            ])

        self.assertEqual(reply.content, "Hello!")
        self.assertEqual(len(requests), 1)
        request = requests[0]
        self.assertEqual(str(request.url), "https://api.openai.com/v1/chat/completions")
        self.assertEqual(request.headers["authorization"], "Bearer test-openai-key")
        body = json.loads(request.content)
        self.assertEqual(body["model"], "gpt-6-luna")
        self.assertEqual(body["reasoning_effort"], "none")
        self.assertEqual(body["max_completion_tokens"], 123)
        self.assertNotIn("max_tokens", body)
        self.assertEqual(body["temperature"], config.language_model.temperature)
        self.assertEqual(body["messages"][-1], {"role": "user", "content": "Hello"})


if __name__ == "__main__":
    unittest.main()
