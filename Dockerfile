# Stage 1: Build CSS
FROM node:24-alpine AS css
WORKDIR /app
COPY package.json package-lock.json ./
COPY src ./src
COPY static ./static
RUN npm ci --no-audit --no-fund && npm run build:css

# Stage 2: Build Go binary
FROM golang:1.27-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=css /app/static/tailwind.css ./static/tailwind.css
RUN CGO_ENABLED=0 go build -o meetup .

# Stage 3: Runtime
FROM alpine:3.24
RUN apk add --no-cache wget
WORKDIR /app
COPY --from=builder /build/meetup .
COPY --from=builder /build/static ./static
EXPOSE 6280
VOLUME ["/app/media", "/db"]
ENV PORT=6280
ENTRYPOINT ["/app/meetup"]
