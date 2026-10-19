ARG BUILD_FROM=ghcr.io/home-assistant/amd64-base:3.21
FROM $BUILD_FROM

WORKDIR /app

RUN apk add --no-cache nodejs npm

COPY package.json package-lock.json tsconfig.json ./
COPY version.json ./version.json
COPY src ./src
RUN npm ci \
  && npm run build \
  && npm prune --omit=dev \
  && npm cache clean --force

COPY run /run.sh
RUN chmod a+x /run.sh

# The chat-app bridge (`ha-nova serve`) and its skills, version-locked to this
# image: .github/workflows/app-image.yml stages them into bridge/ per arch
# (scripts/release/stage-app-bridge.sh). A local Supervisor build copies only
# the placeholder, so chat apps stay unavailable there.
COPY bridge /opt/ha-nova
# Chat apps stay off until the App release that ships the integration (B3)
# sets this to 1; a dev or E2E build can opt in with --build-arg.
ARG HA_NOVA_CHAT_APPS=0
ENV HA_NOVA_CHAT_APPS=$HA_NOVA_CHAT_APPS

CMD ["/run.sh"]
