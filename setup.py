from setuptools import setup, find_packages

setup(
    name="zns-chatbot",
    version="0.2",
    packages=find_packages(),
    install_requires=[
        "python-telegram-bot[job-queue]==20.3",
        "tornado==6.3",
        "pyyaml==6.0",
        "pillow==9.5",
        "numpy==1.24",
        "pymongo==4.3.3",
        "aiofiles==23.1",
        "pydantic==1.10",
        "portalocker==2.7",
    ],
)
