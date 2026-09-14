#!/bin/bash
curl -s -X POST http://localhost:8080/api/auth/token \
  -H "Content-Type: application/json" \
  -d '{"app_key":"demo_key","app_secret":"demo_secret"}'
