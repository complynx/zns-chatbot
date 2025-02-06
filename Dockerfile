# Use an official Python runtime as a parent image
FROM python:3.11-slim-buster

RUN pip install torch==2.3.1 torchvision==0.18.1 torchaudio==2.3.1 --index-url https://download.pytorch.org/whl/cpu

# Install cmake and compiler
RUN apt-get update && apt-get install -y \
    cmake \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# install gl
RUN apt-get update && apt-get install -y \
    libgl1-mesa-glx \
    libglib2.0-0 \
    && rm -rf /var/lib/apt/lists/*

# Set the working directory to /app
WORKDIR /app

# Copy the requirements file or setup.py into the container at /app
COPY setup.py /app

# Install any needed packages specified in setup.py
RUN python setup.py install \
    # Remove compiler and related build tools after installation
    && apt-get purge -y --auto-remove cmake build-essential \
    && rm -rf /var/lib/apt/lists/*

# Copy the current directory contents into the container at /app
COPY . /app

# Bootstrap RAG model
RUN python -m zns-chatbot.plugins.assistant

# Make port 80 available to the world outside this container
EXPOSE 80

# Define environment variable
ENV NAME=zns-chatbot

# Run python -m zns-chatbot.main when the container launches
CMD ["sh", "-c", "TELEGRAM_TOKEN=$TELEGRAM_TOKEN python -m zns-chatbot.main"]
