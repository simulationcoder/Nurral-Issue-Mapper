# Page, then binary with the page embedded, then a small runtime image.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# The golden graph the parity test reads lives outside web/; the build does not need it.
RUN npx vite build

FROM golang:1.25-alpine AS bin
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /issue-mapper ./cmd/issue-mapper

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 mapper
USER mapper
COPY --from=bin /issue-mapper /usr/local/bin/issue-mapper
EXPOSE 8080
# Reads GITHUB_TOKEN and ISSUE_MAPPER_REPO from the environment. It binds to all
# interfaces inside the container; publish the port to loopback only (-p 127.0.0.1:...).
ENTRYPOINT ["issue-mapper"]
CMD ["serve", "--addr", "0.0.0.0:8080"]
