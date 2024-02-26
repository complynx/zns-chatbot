from setuptools import setup, find_packages

setup(
    name="zns-chatbot",
    version="0.3",
    packages=find_packages(),
    install_requires=[
        "python-telegram-bot[job-queue]>=20.8",
        "fluent.runtime>=0.4",
        "pyyaml>=6.0",
        "pillow>=9.5",
        "motor>=3.1",
        "httpx~=0.26.0",
        "pydantic>=1.10,<2.0",
        "openai>=1.3.3",
        "tiktoken>=0.5.1"
    ],
)
