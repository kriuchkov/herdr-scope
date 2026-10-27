# Third-party notices

## Ported portions

Parts of Herdr Scope come from software distributed under the MIT License: the glyph
tables (vendor marks and their font codepoints, state marks, spinner frames), the colour
palette, the sidebar's token names and the layout of the managed blocks in Herdr's
`config.toml` are ported from it to Go, and the bundled icon font
`dist/HerdrAgentIconsMax-Regular.ttf` is derived from it. Its copyright and permission notice:

```text
MIT License

Copyright (c) 2025 qintmb
Copyright (c) 2026 herdr-kit contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## Fonts (`dist/`)

- `dist/HerdrAgentIconsMax-Regular.ttf` — the icon font, built from the vendor marks below and
  covered by the notice above. The font and its one-codepoint-per-logo idea are forked from
  [qintmb/herdr-icon-agent-ui](https://github.com/qintmb/herdr-icon-agent-ui) (MIT). It
  contains no JetBrains Mono outlines; only vertical metrics are copied from that face.
- `dist/JetBrainsMonoHerdr-Regular.ttf` — JetBrains Mono v2.304 (© 2020 The JetBrains Mono
  Project Authors) with the icons patched in and the family renamed to
  "JetBrains Mono Herdr", as the SIL Open Font License 1.1 requires for a modified build. The
  license ships beside it as `dist/OFL.txt`; the font's own copyright and license name
  records are intact. Source: https://github.com/JetBrains/JetBrainsMono

## Vendor marks

The marks in the icon font identify third-party products and imply no affiliation or
endorsement. Sources:

| Mark | Source or owner |
| --- | --- |
| claude | Anthropic `cwc-workshops` (Apache-2.0) |
| codex | OpenAI `codex` (Apache-2.0) |
| opencode | anomalyco `opencode` (MIT) |
| omp | can1357 `oh-my-pi` (MIT) |
| cline | Cline (MIT) |
| mastracode | MastraCode (MIT) |
| kimi | Moonshot AI Kimi Code CLI (Apache-2.0) |
| kilo | Kilo Code CLI (Apache-2.0) |
| maki | Maki (MIT) |
| pi | Pi coding agent |
| hermes | Hermes Agent |
| cursor | Cursor (proprietary) |
| copilot | GitHub Copilot (proprietary) |
| deepseek | DeepSeek (proprietary) |
| gemini | Google Gemini (proprietary) |
| gpt | OpenAI (proprietary) |
| qwen | Alibaba Qwen (proprietary) |
| grok | xAI Grok (proprietary) |
| agy | Google Antigravity (proprietary) |
| kiro | AWS Kiro (proprietary) |
| glm | Z.ai / GLM (proprietary) |
| amp | Amp (proprietary) |
| devin | Cognition Devin (proprietary) |
| qodercli | Alibaba Qoder (proprietary) |
| kimchi | CAST AI Kimchi (proprietary) |
| muse | Meta Platforms / Muse Code (proprietary) |
| crush | Charmbracelet, Inc. / Crush |

Marks for `cursor`, `opencode`, `hermes`, `copilot`, `deepseek`, `gemini`, `gpt`, `qwen`,
`agy`, `kiro`, `glm` and `muse` were taken from
[lobehub/lobe-icons](https://github.com/lobehub/lobe-icons) (MIT); that license covers the
packaging, not the trademarks of the depicted brands.

## Go modules

Linked into the binary; each module's license text ships with its source in the Go module cache.

| Module | License |
| --- | --- |
| github.com/cyperx84/herdr-api | MIT |
| github.com/Microsoft/go-winio | MIT |
| github.com/BurntSushi/toml | MIT |
| github.com/spf13/cobra, github.com/spf13/pflag | Apache-2.0, BSD-3-Clause |
| golang.org/x/sys, golang.org/x/term | BSD-3-Clause |
| github.com/stretchr/testify (tests only) | MIT |
