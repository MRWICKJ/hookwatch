FROM node:20-alpine AS frontend-builder
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

FROM golang:1.23-alpine AS server-builder
WORKDIR /app
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ .
RUN CGO_ENABLED=0 go build -o hookwatch .

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=server-builder /app/hookwatch .
COPY --from=frontend-builder /app/dist ./frontend-dist
EXPOSE 8877
ENTRYPOINT ["./hookwatch"]
CMD ["--port", "8877", "--static-dir", "./frontend-dist"]
