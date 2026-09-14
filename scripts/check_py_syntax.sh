#!/bin/bash
docker exec cs_agent python -c 'import main; print("Python syntax OK")' 2>&1 | tail -15
