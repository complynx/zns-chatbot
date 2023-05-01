from setuptools import setup, find_packages

setup(
    name="zns-chatbot",
    version="0.1",
    packages=find_packages(),
    install_requires=[
        "python-telegram-bot==20.2",
        "tornado==6.2",
        "pyyaml==6.0",
        "pillow==9.5",
        "numpy==1.24.2"
    ],
)
