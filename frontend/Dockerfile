FROM node:24-alpine AS test

WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
CMD ["sh", "-c", "npm test && npm run build"]

FROM node:24-alpine AS build

WORKDIR /app
COPY package*.json ./
RUN npm ci
COPY . .
ARG VITE_KEYCLOAK_URL=http://localhost:8080
ARG VITE_KEYCLOAK_REALM=encurta
ARG VITE_KEYCLOAK_CLIENT_ID=encurta-frontend
ENV VITE_KEYCLOAK_URL=$VITE_KEYCLOAK_URL \
    VITE_KEYCLOAK_REALM=$VITE_KEYCLOAK_REALM \
    VITE_KEYCLOAK_CLIENT_ID=$VITE_KEYCLOAK_CLIENT_ID
RUN npm run build

FROM nginxinc/nginx-unprivileged:1.29-alpine AS runtime

COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 8080