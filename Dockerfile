# Stage 1: Build frontend assets
FROM node:22-alpine AS node-builder
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY build.js ./
COPY public/ ./public/
COPY src/ ./src/
RUN npm run build:prod

# Stage 2: Build Go binary
FROM golang:1.23-alpine AS go-builder
WORKDIR /app
COPY mqtt/go.mod mqtt/go.sum ./
RUN go mod download
COPY mqtt/ ./
COPY --from=node-builder /app/mqtt/dist ./dist
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags '-w -s' -o mqtt .

# Stage 3: Minimal runtime image
FROM scratch
COPY --from=go-builder /app/mqtt /mqtt
EXPOSE 8910
EXPOSE 1883
ENTRYPOINT ["/mqtt"]
