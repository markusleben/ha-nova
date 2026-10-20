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
# The ha_nova Home Assistant integration (chat apps): the App copies it into
# /config/custom_components/ha_nova when the owner turns chat apps on.
COPY integration/ha_nova /app/integration/ha_nova
# Chat apps are available (the owner still turns them on); a local Supervisor
# build without a staged bridge reports them unavailable.
ARG HA_NOVA_CHAT_APPS=1
ENV HA_NOVA_CHAT_APPS=$HA_NOVA_CHAT_APPS

CMD ["/run.sh"]
