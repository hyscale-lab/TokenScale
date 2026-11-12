#!/bin/bash

for i in {1..100}
do
  curl -k http://localhost:8888/v1/completions \
    -H "Content-Type: application/json" \
    -d '{
      "model": "facebook/opt-125m",
      "prompt": "Say this is a test",
      "max_tokens": 7,
      "temperature": 0
    }' &
  sleep 5
done

wait

