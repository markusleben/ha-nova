ARG BUILD_FROM=ghcr.io/home-assistant/amd64-base:3.21
FROM $BUILD_FROM

WORKDIR /app

RUN apk add --no-cache nodejs npm

COPY package.json package-lock.json tsconfig.json ./
COPY src ./src
RUN npm ci \
  && npm run build \
  && npm prune --omit=dev \
  && npm cache clean --force

COPY app/run /run.sh
RUN chmod a+x /run.sh

CMD ["/run.sh"]
