#!/bin/bash
# claude-kiro: launch Claude Code talking to the local ultimate-kiro bridge.
# Usage: claude-kiro            (new chat, then /model to pick Kiro Opus)
#        claude-kiro --continue (resume last chat through Kiro)
export ANTHROPIC_BASE_URL=http://127.0.0.1:3456
export ANTHROPIC_AUTH_TOKEN=ultimate-local-dummy
export ANTHROPIC_API_KEY=
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
exec claude "$@"
