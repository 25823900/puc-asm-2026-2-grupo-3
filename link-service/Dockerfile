FROM node:24-alpine AS test

WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY --chown=node:node src ./src
COPY --chown=node:node test ./test
COPY --chown=node:node eslint.config.js ./eslint.config.js
USER node
CMD ["sh", "-c", "npm run lint && npm test"]

FROM node:24-alpine AS runtime

WORKDIR /app
ENV NODE_ENV=production

COPY package*.json ./
RUN npm ci --omit=dev

COPY src ./src

USER node
EXPOSE 3010
CMD ["node", "--import", "./src/telemetry.js", "src/server.js"]
