#!/bin/bash
echo "Requesting Prometheus snapshot before shutdown..."
curl -X POST http://localhost:9090/api/v1/admin/tsdb/snapshot
echo "Snapshot request sent."
