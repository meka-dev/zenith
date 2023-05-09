#!/usr/bin/env bash

set -euo pipefail

go install github.com/k1LoW/tbls@latest
tbls doc --rm-dist --sort "$PGCONNSTRING" ./docs
