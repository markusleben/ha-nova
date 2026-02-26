ARG BUILD_FROM
FROM $BUILD_FROM

WORKDIR /app

RUN apk add --no-cache nodejs npm

COPY package.json package-lock.json tsconfig.json ./
COPY src ./src
RUN npm ci \
  && npm run build \
  && npm prune --omit=dev \
  && npm cache clean --force

COPY addon/run /run
RUN chmod a+x /run

CMD ["/run"]
