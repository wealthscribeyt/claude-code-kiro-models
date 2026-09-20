# ultimate-kiro on Oracle Ubuntu — install guide

## 1. Send files to the instance (from Windows PowerShell)
```powershell
scp dist-ubuntu/ultimate-kiro-linux-* ubuntu@YOUR-INSTANCE-IP:~/ultimate-kiro/
scp dist-ubuntu/ultimate-kiro.service dist-ubuntu/kiro.env.example ubuntu@YOUR-INSTANCE-IP:~/ultimate-kiro/
```
Pick the binary for your shape: Ampere A1 = **arm64**, Intel/AMD = **amd64**.
Delete the other one on the server to avoid confusion.

## 2. On the instance (SSH)
```bash
cd ~/ultimate-kiro
chmod +x ultimate-kiro-linux-*
cp kiro.env.example kiro.env
nano kiro.env   # paste your ksk_ Pro+ key (app.kiro.dev -> API Keys)
chmod 600 kiro.env

# keep only your arch:
# Ampere ARM:  mv ultimate-kiro-linux-arm64 ultimate-kiro
# Intel/AMD:   mv ultimate-kiro-linux-amd64 ultimate-kiro
# then: rm ultimate-kiro-linux-*

# install as a service (auto-start, auto-restart):
sudo cp ultimate-kiro.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now ultimate-kiro
systemctl status ultimate-kiro --no-pager | head -12

# verify (on the instance):
curl -s http://127.0.0.1:3456/v1/models | head -c 300; echo
```

## 3. Connect Claude Code
**A) Claude Code runs ON the instance (recommended):**
```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:3456
export ANTHROPIC_AUTH_TOKEN=anything
export ANTHROPIC_API_KEY=
export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1
claude   # /model -> pick claude-opus-5[1m]
```

**B) Claude Code stays on Windows, bridge on Oracle:**
On Windows, one SSH tunnel (keep open):
```powershell
ssh -N -L 3456:127.0.0.1:3456 ubuntu@YOUR-INSTANCE-IP
```
Then your existing `claude-kiro.cmd` works unchanged.

## Notes
- Headless auth uses `KIRO_API_KEY` (ksk_…) — no browser login on the server.
- With API-key auth, model discovery is skipped; the built-in catalog
  (opus-5, sonnet-5, [1m] 1M variants, gpt-5.6, auto) is used. Fine.
- Bridge binds 127.0.0.1 only. Never expose 3456 to the internet without
  setting `KIROCC_API_KEY` + Oracle firewall rules.
- Logs: `~/ultimate-kiro/ultimate-kiro.log` — look for
  `thinking budget mapped to native effort ... effort=max` as proof of max brain.
- Check brain level live: `grep effort ~/ultimate-kiro/ultimate-kiro.log | tail -3`
