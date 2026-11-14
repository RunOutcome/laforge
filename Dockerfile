# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: 2026 COR Holdings, LLC
#
# LaForge in a container: the forge, a shell toolbox, and AI agent CLIs.
#   make image                     # builds laforge:local
#   la up --container              # runs it, mounts this folder at /workspace
#
# Agent CLIs under open licences are built in (BUNDLED). Claude Code and the
# Copilot CLI have proprietary licences, so the image does not carry them:
# the entrypoint installs them from npm into the persistent home volume on
# first start (ON_FIRST_START), as the user's own install.

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/RunOutcome/laforge/cli.Version=${VERSION}" -o /out/la ./cmd/la

FROM node:22-bookworm-slim
ARG BUNDLED="@openai/codex @google/gemini-cli opencode-ai"
RUN apt-get update && apt-get install -y --no-install-recommends \
      bash zsh git curl ca-certificates openssh-client less nano vim-tiny \
      ripgrep fd-find jq procps tini locales python3 python3-pip make \
    && sed -i 's/# en_US.UTF-8/en_US.UTF-8/' /etc/locale.gen && locale-gen \
    && rm -rf /var/lib/apt/lists/* \
    && npm install -g --no-fund --no-audit ${BUNDLED} \
    && npm cache clean --force \
    && userdel -r node \
    && useradd -m -u 1000 -s /bin/zsh forge \
    && mkdir -p /workspace && chown forge:forge /workspace
COPY --from=build /out/la /usr/local/bin/la
COPY docker/entrypoint.sh /usr/local/bin/laforge-entrypoint
COPY docker/zshrc /etc/zsh/zshrc.laforge
RUN cat /etc/zsh/zshrc.laforge >> /etc/zsh/zshrc && chmod +x /usr/local/bin/laforge-entrypoint

USER forge
ENV HOME=/home/forge \
    SHELL=/bin/zsh \
    LANG=en_US.UTF-8 \
    LAFORGE_HOME=/home/forge/.laforge/forge \
    LAFORGE_LISTEN=0.0.0.0 \
    LAFORGE_NO_BROWSER=1 \
    NPM_CONFIG_PREFIX=/home/forge/.local \
    PATH=/home/forge/.local/bin:/usr/local/bin:/usr/bin:/bin \
    ON_FIRST_START="@anthropic-ai/claude-code @github/copilot"
WORKDIR /workspace
VOLUME ["/home/forge"]
EXPOSE 4646
HEALTHCHECK --interval=15s --timeout=3s CMD curl -fsS http://127.0.0.1:4646/api/health || exit 1
ENTRYPOINT ["tini", "--", "laforge-entrypoint"]
CMD ["la", "serve", "--port", "4646", "--cwd", "/workspace"]
