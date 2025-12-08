FROM python:3.11-slim-bookworm AS builder

# Install uv
COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /usr/local/bin/

# Set UV cache directory
ENV UV_CACHE_DIR=/tmp/uv-cache PIP_NO_CACHE_DIR=1

# Minimal native deps for runtime plus lightweight build chain for insightface
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1-mesa-glx \
    libglib2.0-0 \
    libgomp1 \
    build-essential \
    python3-dev \
    && rm -rf /var/lib/apt/lists/*

# Create virtual environment
RUN uv venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Install PyTorch first with specific index URL
RUN uv pip install torch==2.3.1 torchvision==0.18.1 torchaudio==2.3.1 --index-url https://download.pytorch.org/whl/cpu

# Install other dependencies
RUN uv pip install albumentations==1.4.3 Cython

# Install insightface from wheels (skip source build to save space)
RUN uv pip install "insightface>=0.7,<0.8"

# Copy pyproject.toml and install application
COPY pyproject.toml /tmp/
RUN cd /tmp && uv pip install .

# Clean up
RUN rm -rf /tmp/uv-cache /root/.cache /tmp/*

FROM python:3.11-slim-bookworm

# Install minimal runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1-mesa-glx \
    libglib2.0-0 \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/*

# Copy virtual environment from builder
COPY --from=builder /opt/venv /opt/venv
ENV PATH="/opt/venv/bin:$PATH"

# Set the working directory
WORKDIR /app

# Copy application code
COPY . /app

# Create non-root user
RUN groupadd -r appuser && useradd -r -g appuser appuser
RUN chown -R appuser:appuser /app
USER appuser

# Make port 80 available
EXPOSE 80

# Define environment variable
ENV NAME=zns-chatbot

# Run the application
CMD ["python", "-m", "zns-chatbot.main"]
