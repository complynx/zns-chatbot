from setuptools import setup, find_packages

setup(
    name="zns-chatbot",
    version="0.3",
    packages=find_packages(),
    install_requires=[
        "python-telegram-bot[job-queue]>=21.0.1",
        "opencv-python==4.9.0.80"
        "numpy==1.26.4"
        "tornado==6.4",
        "fluent.runtime>=0.4",
        "pyyaml>=6.0",
        "pillow>=10.2",
        "motor>=3.1",
        "httpx~=0.27.0",
        "pydantic>=1.10,<2.0",
        "openai>=1.3.3",
        "tiktoken>=0.5.1"
    ],
)
