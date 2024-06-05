from setuptools import setup, find_packages

setup(
    name="zns-chatbot",
    version="0.3",
    packages=find_packages(),
    install_requires=[
        "python-telegram-bot[job-queue]>=21.0.1",
        # "opencv-python>=4.9.0.1",
        "dlib>=19.24.0",
        "numpy==1.26.4,<2.0",
        "tornado==6.4",
        "fluent.runtime>=0.4",
        "pyyaml>=6.0",
        "pillow>=10.2",
        "motor>=3.1",
        "httpx~=0.27.0",
        "pydantic>=2.7",
        "pydantic-settings>=2.3",
        "openai>=1.30.1",
        "tiktoken>=0.7.0"
        "langchain>=0.2.2",
        "langchain-community>=0.2.2",
        "langchain-openai>=0.1.8",
        "langchain-huggingface>=0.0.2",
    ],
)
