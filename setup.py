from setuptools import setup, find_packages

setup(
    name="zns-chatbot",
    version="0.3",
    packages=find_packages(),
    install_requires=[
        "packaging==23.2",
        "python-telegram-bot[job-queue]>=21.0.1",
        # "opencv-python>=4.9.0.1",
        "dlib>=19.24.0",
        "tokenizers>=0.19.1,<0.20",
        "numpy==1.26.4,<2.0",
        "tornado==6.4",
        "fluent.runtime>=0.4",
        "pyyaml>=6.0",
        "pillow>=10.4",
        "motor>=3.1",
        "httpx~=0.27.0",
        "pydantic>=2.7",
        "pydantic-settings>=2.3",
        "openai>=1.30.1",
        "tiktoken>=0.7.0",
        "langchain>=0.2.2",
        "langchain-community>=0.2.2",
        "langchain-openai>=0.1.8",
        "langchain-huggingface>=0.0.2",
        "faiss-cpu==1.8.0",
    ],
    dependency_links=[
        "https://download.pytorch.org/whl/cpu"
    ],
)
