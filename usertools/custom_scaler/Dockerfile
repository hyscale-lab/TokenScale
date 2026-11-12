# Use a lightweight Python base image
FROM python:3.10-slim

# Install curl just in case you want debugging in the container
RUN apt-get update && apt-get install -y --no-install-recommends curl && \
    rm -rf /var/lib/apt/lists/*

# Set work directory
WORKDIR /app

# Copy your Python script into the container
COPY main.py /app/scaler.py

# Install Python dependencies
# Add `requests` or any other packages your script needs
RUN pip install --no-cache-dir requests

# Set entrypoint to run the script with passed arguments
ENTRYPOINT ["python", "/app/scaler.py"]